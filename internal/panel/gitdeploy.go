package panel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// Git deploys clone a repository on the panel and turn the working tree into an ordinary
// artifact. The panel reaches out to a URL a customer typed, so cloning is locked down:
// HTTPS only, and the dialer refuses private, loopback, link-local and other internal addresses
// at connect time (after DNS resolution, so DNS rebinding cannot sneak past it).

const gitCloneTimeout = 2 * time.Minute

var errGitBlocked = errors.New("that address is on a private or internal network")

// blockedIP reports addresses the panel must never connect to on a customer's behalf.
func blockedIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 0, // "this" network
			v4[0] == 100 && v4[1]&0xc0 == 64,             // 100.64.0.0/10 carrier-grade NAT
			v4[0] == 192 && v4[1] == 0 && v4[2] == 0,     // 192.0.0.0/24 IETF
			v4[0] == 198 && (v4[1] == 18 || v4[1] == 19), // 198.18.0.0/15 benchmarking
			v4[0] >= 240: // reserved and broadcast
			return true
		}
	}
	return false
}

func denyInternal(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if blockedIP(net.ParseIP(host)) {
		return errGitBlocked
	}
	return nil
}

func init() {
	safe := &http.Client{
		Timeout: gitCloneTimeout,
		Transport: &http.Transport{
			Proxy:               nil, // never through a proxy that could reach internal hosts for us
			DialContext:         (&net.Dialer{Timeout: 10 * time.Second, Control: denyInternal}).DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
			MaxIdleConns:        4,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" {
				return errors.New("redirect away from https")
			}
			return nil
		},
	}
	// Only https is installed for go-git's clone; http, ssh, git:// and file:// are not offered.
	client.InstallProtocol("https", githttp.NewClient(safe))
}

type gitSource struct {
	URL, Ref, Commit string
}

func parseGitURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || len(raw) > 500 {
		return "", errBadRequest("Use an https:// git URL, like https://github.com/you/your-bot.git")
	}
	if u.User != nil {
		return "", errBadRequest("Put access tokens in the token field, not in the URL.")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && blockedIP(ip) {
		return "", errBadRequest("That address is on a private network.")
	}
	return u.String(), nil
}

func validRef(s string) bool {
	if s == "" || len(s) > 200 || strings.Contains(s, "..") || strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		if !(r == '/' || r == '-' || r == '_' || r == '.' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

// cloneToArtifact shallow-clones a branch or tag and stores its files as an artifact.
func cloneToArtifact(ctx context.Context, dir, repoURL, ref, token string, limit int64) (artifact, gitSource, error) {
	tmp, err := os.MkdirTemp("", "mechon-git-*")
	if err != nil {
		return artifact{}, gitSource{}, err
	}
	defer os.RemoveAll(tmp)
	ctx, cancel := context.WithTimeout(ctx, gitCloneTimeout)
	defer cancel()

	opts := &git.CloneOptions{URL: repoURL, Depth: 1, SingleBranch: true, Tags: git.NoTags}
	if token != "" {
		opts.Auth = &githttp.BasicAuth{Username: "x-access-token", Password: token}
	}
	try := func(name plumbing.ReferenceName) (*git.Repository, error) {
		os.RemoveAll(tmp)
		opts.ReferenceName = name
		return git.PlainCloneContext(ctx, tmp, false, opts)
	}
	var repo *git.Repository
	if ref == "" {
		repo, err = try("")
	} else if repo, err = try(plumbing.NewBranchReferenceName(ref)); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		repo, err = try(plumbing.NewTagReferenceName(ref))
	}
	if err != nil {
		return artifact{}, gitSource{}, gitError(err, ref)
	}
	head, err := repo.Head()
	if err != nil {
		return artifact{}, gitSource{}, errBadRequest("The repository has no commits on that branch.")
	}

	var entries []entry
	var total int64
	err = filepath.WalkDir(tmp, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(tmp, p)
		if rel == "." {
			return nil
		}
		if d.Name() == ".git" && d.IsDir() {
			return filepath.SkipDir
		}
		rel = filepath.ToSlash(rel)
		switch {
		case d.IsDir():
			entries = append(entries, entry{name: rel, dir: true, mode: 0o755})
		case d.Type().IsRegular():
			info, err := d.Info()
			if err != nil {
				return err
			}
			if total += info.Size(); total > limit {
				return errBadArchive(fmt.Sprintf("the repository is larger than this bot's %d MB disk", limit>>20))
			}
			full := p
			entries = append(entries, entry{name: rel, mode: info.Mode().Perm(), size: info.Size(), open: func() (io.ReadCloser, error) { return os.Open(full) }})
		default:
			// Symlinks and other special files are left out; they could point anywhere.
		}
		return nil
	})
	if err != nil {
		return artifact{}, gitSource{}, err
	}
	art, err := writeNormalised(dir, entries, limit)
	if err != nil {
		return artifact{}, gitSource{}, err
	}
	shown := ref
	if shown == "" {
		shown = head.Name().Short()
	}
	return art, gitSource{URL: repoURL, Ref: shown, Commit: head.Hash().String()}, nil
}

func gitError(err error, ref string) error {
	msg := err.Error()
	switch {
	case errors.Is(err, errGitBlocked) || strings.Contains(msg, errGitBlocked.Error()):
		return errBadRequest("That repository's address is on a private network.")
	case errors.Is(err, context.DeadlineExceeded):
		return errBadRequest("Cloning took longer than two minutes. Is the repository very large?")
	case strings.Contains(msg, "authentication required"), strings.Contains(msg, "authorization failed"), strings.Contains(msg, "repository not found"):
		return errBadRequest("The repository was not found, or it is private and needs an access token.")
	case strings.Contains(msg, "couldn't find remote ref"), strings.Contains(msg, "reference not found"):
		return errBadRequest(fmt.Sprintf("There is no branch or tag called %q.", ref))
	}
	return errBadRequest("Could not clone the repository: " + truncate(msg, 200))
}
