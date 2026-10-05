package panel

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jub0t/mechon/internal/auth"
	"github.com/jub0t/mechon/internal/db"
)

// Uploads are normalised into one format before anything else sees them: a gzipped tar of plain
// files and directories with clean relative paths. Links, devices, absolute paths and ".." are
// rejected outright. If everything sits in one top-level folder (as when a folder is zipped),
// that folder is stripped. The agent validates again on its side.

const maxArtifactFiles = 20000

type artifact struct {
	SHA256 string
	Bytes  int64
}

type entry struct {
	name string
	mode fs.FileMode
	dir  bool
	size int64
	open func() (io.ReadCloser, error)
}

var errBadArchive = func(msg string) error { return errBadRequest("That archive cannot be deployed: " + msg) }

// storeUpload reads a .zip or .tar.gz from src (at most limit bytes unpacked) and stores the
// normalised artifact in dir, named by its SHA-256.
func storeUpload(dir string, src io.Reader, limit int64) (artifact, error) {
	tmp, err := os.CreateTemp(dir, "upload-*")
	if err != nil {
		return artifact{}, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	n, err := io.Copy(tmp, src)
	if err != nil {
		return artifact{}, err
	}
	if n == 0 {
		return artifact{}, errBadArchive("the file is empty")
	}

	var head [4]byte
	if _, err := tmp.ReadAt(head[:], 0); err != nil {
		return artifact{}, errBadArchive("the file is too short")
	}
	var entries []entry
	switch {
	case bytes.HasPrefix(head[:], []byte("PK\x03\x04")):
		entries, err = zipEntries(tmp, n)
	case bytes.HasPrefix(head[:], []byte{0x1f, 0x8b}):
		entries, err = tarGzEntries(tmp, limit)
	default:
		return artifact{}, errBadArchive("upload a .zip or a .tar.gz")
	}
	if err != nil {
		return artifact{}, err
	}
	return writeNormalised(dir, stripCommonRoot(entries), limit)
}

func cleanName(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(name, "/") || (len(name) > 1 && name[1] == ':') {
		return "", errBadArchive(fmt.Sprintf("%q is an absolute path", name))
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", errBadArchive(fmt.Sprintf("%q points outside the archive", name))
		}
	}
	clean := path.Clean(name)
	if clean == "." || clean == "" {
		return "", nil
	}
	// macOS zip droppings carry nothing a bot needs.
	if path.Base(clean) == ".DS_Store" || slices.Contains(strings.Split(clean, "/"), "__MACOSX") {
		return "", nil
	}
	return clean, nil
}

func zipEntries(f *os.File, size int64) ([]entry, error) {
	zr, err := zip.NewReader(f, size)
	if err != nil {
		return nil, errBadArchive("it is not a valid zip file")
	}
	if len(zr.File) > maxArtifactFiles {
		return nil, errBadArchive("it has too many files")
	}
	var out []entry
	for _, zf := range zr.File {
		name, err := cleanName(zf.Name)
		if err != nil {
			return nil, err
		}
		if name == "" {
			continue
		}
		mode := zf.Mode()
		switch {
		case mode.IsDir():
			out = append(out, entry{name: name, dir: true, mode: 0o755})
		case mode.IsRegular():
			zf := zf
			out = append(out, entry{name: name, mode: mode.Perm(), size: int64(zf.UncompressedSize64), open: zf.Open})
		default:
			return nil, errBadArchive(fmt.Sprintf("%q is a link or special file", zf.Name))
		}
	}
	return out, nil
}

// tarGzEntries buffers file bodies in memory, stopping as soon as the unpacked total passes limit
// so a gzip bomb cannot exhaust memory.
func tarGzEntries(f *os.File, limit int64) ([]entry, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	gz, err := gzip.NewReader(bufio.NewReader(f))
	if err != nil {
		return nil, errBadArchive("it is not a valid gzip file")
	}
	tr := tar.NewReader(gz)
	var out []entry
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errBadArchive("it is not a valid tar file")
		}
		if len(out) >= maxArtifactFiles {
			return nil, errBadArchive("it has too many files")
		}
		name, err := cleanName(h.Name)
		if err != nil {
			return nil, err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if name != "" {
				out = append(out, entry{name: name, dir: true, mode: 0o755})
			}
		case tar.TypeReg:
			body, err := io.ReadAll(io.LimitReader(tr, limit-total+1))
			if err != nil {
				return nil, errBadArchive("it is truncated")
			}
			if total += int64(len(body)); total > limit {
				return nil, errBadArchive(fmt.Sprintf("it unpacks to more than this bot's %d MB disk", limit>>20))
			}
			if name != "" {
				out = append(out, entry{name: name, mode: fs.FileMode(h.Mode).Perm(), size: int64(len(body)),
					open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }})
			}
		case tar.TypeXGlobalHeader, tar.TypeXHeader:
		default:
			return nil, errBadArchive(fmt.Sprintf("%q is a link or special file", h.Name))
		}
	}
	return out, nil
}

// stripCommonRoot removes a single top-level directory that contains everything.
func stripCommonRoot(entries []entry) []entry {
	root := ""
	for _, e := range entries {
		first, rest, nested := strings.Cut(e.name, "/")
		if !nested && !e.dir {
			return entries // a file at the top level: nothing to strip
		}
		if root == "" {
			root = first
		} else if root != first {
			return entries
		}
		_ = rest
	}
	if root == "" {
		return entries
	}
	out := entries[:0:0]
	for _, e := range entries {
		if e.name == root {
			continue
		}
		e.name = strings.TrimPrefix(e.name, root+"/")
		out = append(out, e)
	}
	return out
}

func writeNormalised(dir string, entries []entry, limit int64) (artifact, error) {
	var total int64
	files := 0
	for _, e := range entries {
		total += e.size
		if !e.dir {
			files++
		}
	}
	if files == 0 {
		return artifact{}, errBadArchive("it has no files")
	}
	if total > limit {
		return artifact{}, errBadArchive(fmt.Sprintf("it unpacks to %d MB, more than this bot's %d MB disk", total>>20, limit>>20))
	}

	out, err := os.CreateTemp(dir, "artifact-*")
	if err != nil {
		return artifact{}, err
	}
	defer os.Remove(out.Name())
	defer out.Close()
	sum := sha256.New()
	counter := &countWriter{}
	gz, _ := gzip.NewWriterLevel(io.MultiWriter(out, sum, counter), gzip.BestSpeed)
	tw := tar.NewWriter(gz)
	epoch := time.Unix(0, 0)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, ModTime: epoch, Format: tar.FormatPAX}
		if e.dir {
			h.Typeflag, h.Name, h.Mode = tar.TypeDir, e.name+"/", 0o755
			if err := tw.WriteHeader(h); err != nil {
				return artifact{}, err
			}
			continue
		}
		// Keep the executable bit, drop everything else (setuid, sticky, odd permissions).
		h.Typeflag, h.Size, h.Mode = tar.TypeReg, e.size, 0o644
		if e.mode&0o111 != 0 {
			h.Mode = 0o755
		}
		if err := tw.WriteHeader(h); err != nil {
			return artifact{}, err
		}
		rc, err := e.open()
		if err != nil {
			return artifact{}, errBadArchive("a file could not be read")
		}
		n, err := io.Copy(tw, io.LimitReader(rc, e.size))
		rc.Close()
		if err != nil || n != e.size {
			return artifact{}, errBadArchive(fmt.Sprintf("%q is truncated", e.name))
		}
	}
	if err := tw.Close(); err != nil {
		return artifact{}, err
	}
	if err := gz.Close(); err != nil {
		return artifact{}, err
	}
	if err := out.Close(); err != nil {
		return artifact{}, err
	}
	digest := hex.EncodeToString(sum.Sum(nil))
	if err := os.Rename(out.Name(), filepath.Join(dir, digest+".tar.gz")); err != nil {
		return artifact{}, err
	}
	return artifact{SHA256: digest, Bytes: counter.n}, nil
}

type countWriter struct{ n int64 }

func (c *countWriter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

// serveArtifact is GET /agent/v1/artifacts/{id}: a node fetches the code for a deploy of a bot
// placed on it, and nothing else.
func (s *Server) serveArtifact(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		http.Error(w, "missing node token", http.StatusUnauthorized)
		return
	}
	node, err := s.q.GetNodeByTokenHash(r.Context(), auth.HashToken(strings.TrimSpace(token)))
	if err != nil {
		http.Error(w, "unknown node token", http.StatusUnauthorized)
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	sha, err := s.q.DeployBelongsToNode(r.Context(), db.DeployBelongsToNodeParams{ID: id, NodeID: node.ID})
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(s.artifactDir, sha+".tar.gz"))
	if errors.Is(err, fs.ErrNotExist) {
		http.Error(w, "artifact missing on the panel", http.StatusGone)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("X-Mechon-SHA256", sha)
	http.ServeContent(w, r, sha+".tar.gz", time.Unix(0, 0), f)
}
