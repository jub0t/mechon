//go:build linux

package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jub0t/mechon/internal/proto"
)

const installTimeout = 10 * time.Minute

// deploy fetches spec's artifact, installs it in .mechon/next and swaps it into app/. On any
// error the running version is untouched.
func (d *Docker) deploy(ctx context.Context, spec proto.BotSpec) error {
	out := newLineBatcher(func(lines []proto.LogLine) {
		d.emitDeploy(spec, proto.PhaseInstalling, lines, "")
	})
	defer out.Close()

	d.emitDeploy(spec, proto.PhaseFetching, nil, "")
	next := filepath.Join(d.metaDir(spec.BotID), "next")
	if err := os.RemoveAll(next); err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(next)
		}
	}()
	if err := os.Mkdir(next, 0o750); err != nil {
		return err
	}
	if err := os.Lchown(next, spec.UID, spec.UID); err != nil {
		return err
	}
	maxBytes := int64(spec.Limits.DiskMB) * mib
	if err := d.fetchAndExtract(ctx, spec, next, maxBytes); err != nil {
		return err
	}

	d.emitDeploy(spec, proto.PhaseInstalling, nil, "")
	if err := d.ensureImage(ctx, spec.Template.Image, func(l string) { out.Add("system", l) }); err != nil {
		return err
	}
	if strings.TrimSpace(spec.Template.Install) != "" {
		out.Add("system", "running install")
		if err := d.runInstall(ctx, spec, next, out); err != nil {
			return err
		}
		out.Add("system", "install finished")
	}

	// Swap: app → .mechon/prev, next → app. The running container keeps its bind of the old
	// directory until it is recreated.
	app := d.appDir(spec.BotID)
	prev := filepath.Join(d.metaDir(spec.BotID), "prev")
	if err := os.RemoveAll(prev); err != nil {
		return err
	}
	if err := os.Rename(app, prev); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(next, app); err != nil {
		_ = os.Rename(prev, app)
		return err
	}
	ok = true
	return d.writeDeployID(spec.BotID, spec.DeployID)
}

func (d *Docker) fetchAndExtract(ctx context.Context, spec proto.BotSpec, dir string, maxBytes int64) error {
	base, err := url.Parse(d.cfg.PanelURL)
	if err != nil || base.Host == "" {
		return fmt.Errorf("bad panel URL %q", d.cfg.PanelURL)
	}
	ref, err := url.Parse(spec.ArtifactPath)
	if err != nil || ref.IsAbs() || ref.Host != "" {
		return fmt.Errorf("bad artifact path %q", spec.ArtifactPath)
	}
	u := base.ResolveReference(ref)

	tmpDir := filepath.Join(d.dataDir, "tmp")
	f, err := os.CreateTemp(tmpDir, spec.BotID+"-*.tar.gz")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()

	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+d.cfg.NodeToken)
	resp, err := d.http.Do(req)
	if err != nil {
		return fmt.Errorf("download artifact: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download artifact: HTTP %d", resp.StatusCode)
	}
	// The compressed artifact can be no bigger than what it may expand to.
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return fmt.Errorf("download artifact: %w", err)
	}
	if n > maxBytes {
		return fmt.Errorf("artifact is larger than the %d MB disk quota", maxBytes/mib)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, spec.ArtifactSHA256) {
		return fmt.Errorf("artifact sha256 mismatch: got %s, want %s", got, spec.ArtifactSHA256)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := extractTarGz(f, dir, maxBytes, spec.UID); err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	return nil
}

// runInstall runs the template's install script in a throwaway container with the bot's user,
// limits and network, over .mechon/next mounted at /home/container.
func (d *Docker) runInstall(ctx context.Context, spec proto.BotSpec, next string, out *lineBatcher) error {
	name := installName(spec.BotID)
	if c, err := d.api.inspectContainer(ctx, name); err == nil {
		_ = d.api.removeContainer(ctx, c.ID)
	}
	hc := d.limitsHostConfig(spec)
	hc.Binds = []string{next + ":" + containerHome + ":rw"}
	hc.Tmpfs = map[string]string{"/tmp": "rw,nosuid,nodev,size=256m"}
	hc.LogConfig = logConfig{Type: "local", Config: map[string]string{"max-size": "10m", "max-file": "2"}}
	cfg := containerConfig{
		Hostname:   hostname(spec.Name),
		User:       fmt.Sprintf("%d:%d", spec.UID, spec.UID),
		Env:        botEnv(spec),
		Cmd:        []string{"sh", "-c", spec.Template.Install},
		Entrypoint: []string{""},
		Image:      spec.Template.Image,
		WorkingDir: containerHome,
		Labels:     map[string]string{labelInstall: spec.BotID},
		HostConfig: hc,
	}
	id, err := d.api.createContainer(ctx, name, cfg)
	if err != nil {
		return fmt.Errorf("create install container: %w", err)
	}
	// Always clean up, even if ctx is cancelled.
	defer func() {
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = d.api.removeContainer(cctx, id)
	}()

	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()
	if err := d.api.startContainer(ctx, id); err != nil {
		return fmt.Errorf("start install: %w", err)
	}
	logsDone := make(chan struct{})
	go func() {
		defer close(logsDone)
		rc, err := d.api.containerLogs(ctx, id, url.Values{"stdout": {"1"}, "stderr": {"1"}, "follow": {"1"}})
		if err != nil {
			return
		}
		defer rc.Close()
		_ = demuxLines(rc, func(stream string, line []byte) { out.Add(stream, string(line)) })
	}()
	code, err := d.api.waitContainer(ctx, id)
	if ctx.Err() != nil {
		kctx, kcancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = d.api.killContainer(kctx, id)
		kcancel()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("install timed out after %s", installTimeout)
		}
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("install: %w", err)
	}
	select {
	case <-logsDone:
	case <-time.After(5 * time.Second):
	}
	if code != 0 {
		oom := ""
		if c, err := d.api.inspectContainer(ctx, id); err == nil && c.State.OOMKilled {
			oom = " (out of memory)"
		}
		return fmt.Errorf("install exited with code %d%s", code, oom)
	}
	return nil
}

// lineBatcher collects log lines and flushes them every 200 ms or every 100 lines.
type lineBatcher struct {
	mu    sync.Mutex
	lines []proto.LogLine
	flush func([]proto.LogLine)
	stop  chan struct{}
	done  chan struct{}
}

func newLineBatcher(flush func([]proto.LogLine)) *lineBatcher {
	b := &lineBatcher{flush: flush, stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(b.done)
		t := time.NewTicker(200 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				b.Flush()
			case <-b.stop:
				b.Flush()
				return
			}
		}
	}()
	return b
}

func (b *lineBatcher) Add(stream, text string) {
	b.mu.Lock()
	b.lines = append(b.lines, proto.LogLine{T: time.Now().UnixMilli(), Stream: stream, Text: text})
	full := len(b.lines) >= 100
	b.mu.Unlock()
	if full {
		b.Flush()
	}
}

func (b *lineBatcher) Flush() {
	b.mu.Lock()
	lines := b.lines
	b.lines = nil
	b.mu.Unlock()
	if len(lines) > 0 {
		b.flush(lines)
	}
}

func (b *lineBatcher) Close() {
	close(b.stop)
	<-b.done
}
