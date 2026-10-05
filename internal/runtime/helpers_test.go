//go:build linux

package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jub0t/mechon/internal/proto"
)

// needDocker skips unless MECHON_TEST_DOCKER=1 and we are root (loop mounts, iptables).
func needDocker(t *testing.T) {
	t.Helper()
	if os.Getenv("MECHON_TEST_DOCKER") != "1" {
		t.Skip("set MECHON_TEST_DOCKER=1 (as root, on Linux with Docker) to run")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
}

// fakePanel serves artifacts like the panel's /agent/v1/artifacts/{deploy} endpoint.
type fakePanel struct {
	*httptest.Server
	mu    sync.Mutex
	files map[string][]byte
}

const testToken = "node-token-123"

func newFakePanel(t *testing.T) *fakePanel {
	p := &fakePanel{files: map[string][]byte{}}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			http.Error(w, "unauthorized", 401)
			return
		}
		p.mu.Lock()
		b, ok := p.files[r.URL.Path]
		p.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(p.Close)
	return p
}

// publish stores an artifact and points spec at it.
func (p *fakePanel) publish(spec *proto.BotSpec, deployID string, data []byte) {
	path := "/agent/v1/artifacts/" + deployID
	p.mu.Lock()
	p.files[path] = data
	p.mu.Unlock()
	sum := sha256.Sum256(data)
	spec.DeployID = deployID
	spec.ArtifactPath = path
	spec.ArtifactSHA256 = hex.EncodeToString(sum[:])
}

type recorder struct {
	mu      sync.Mutex
	deploys []proto.DeployProgress
	system  []string
}

func (r *recorder) hooks() Hooks {
	return Hooks{
		DeployProgress: func(p proto.DeployProgress) {
			r.mu.Lock()
			r.deploys = append(r.deploys, p)
			r.mu.Unlock()
		},
		SystemLog: func(botID, text string) {
			r.mu.Lock()
			r.system = append(r.system, botID+": "+text)
			r.mu.Unlock()
		},
	}
}

func (r *recorder) phases(deployID string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, p := range r.deploys {
		if p.DeployID == deployID && (len(out) == 0 || out[len(out)-1] != p.Phase) {
			out = append(out, p.Phase)
		}
	}
	return out
}

func (r *recorder) deployText(deployID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
	for _, p := range r.deploys {
		if p.DeployID != deployID {
			continue
		}
		for _, l := range p.Lines {
			b.WriteString(l.Text + "\n")
		}
		if p.Error != "" {
			b.WriteString("ERROR: " + p.Error + "\n")
		}
	}
	return b.String()
}

type testEnv struct {
	t     *testing.T
	d     *Docker
	panel *fakePanel
	rec   *recorder
	ctx   context.Context
}

func newTestEnv(t *testing.T) *testEnv {
	needDocker(t)
	dir, err := os.MkdirTemp("/var/lib", "mechon-test-")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	panel := newFakePanel(t)
	rec := &recorder{}
	d, err := NewDocker(ctx, Config{
		DataDir:   dir,
		PanelURL:  panel.URL,
		NodeToken: testToken,
		Hooks:     rec.hooks(),
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	e := &testEnv{t: t, d: d, panel: panel, rec: rec, ctx: ctx}
	t.Cleanup(func() {
		ids, _ := d.volumeIDs()
		cs, _ := d.api.listContainers(context.Background(), labelBot)
		for _, c := range cs {
			ids = append(ids, c.Labels[labelBot])
		}
		for _, id := range ids {
			if err := d.Remove(context.Background(), id); err != nil {
				t.Errorf("cleanup remove %s: %v", id, err)
			}
		}
		os.RemoveAll(dir)
		cancel()
	})
	return e
}

// deploySimple publishes a tarball with the given files and applies spec with it.
func (e *testEnv) apply(spec proto.BotSpec) Instance {
	e.t.Helper()
	inst, err := e.d.Apply(e.ctx, spec)
	if err != nil {
		e.t.Fatalf("apply %s: %v\ndeploy output:\n%s", spec.BotID, err, e.rec.deployText(spec.DeployID))
	}
	return inst
}

func (e *testEnv) withCode(spec *proto.BotSpec, deployID string, files map[string]string) {
	var entries []tarEntry
	for name, body := range files {
		entries = append(entries, tarEntry{name: name, body: body, mode: 0o755})
	}
	e.panel.publish(spec, deployID, makeTarGz(e.t, entries))
}

// sh runs a host command and returns combined output.
func sh(t *testing.T, cmd string) (string, error) {
	t.Helper()
	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// inBot runs a shell script inside the bot's container (same user, namespaces, cgroup,
// capabilities and seccomp as the bot itself).
func inBot(t *testing.T, botID, script string) (string, error) {
	t.Helper()
	out, err := exec.Command("docker", "exec", containerName(botID), "sh", "-c", script).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func waitFor(t *testing.T, what string, timeout time.Duration, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
