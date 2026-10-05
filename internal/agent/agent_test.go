package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jub0t/mechon/internal/proto"
	"github.com/jub0t/mechon/internal/runtime"
)

const token = "node-secret"

// fakePanel accepts one agent connection at a time and records every frame it sends.
type fakePanel struct {
	*httptest.Server
	t         *testing.T
	frames    chan proto.Envelope
	conns     chan *websocket.Conn
	artifacts map[string][]byte
}

func newFakePanel(t *testing.T) *fakePanel {
	p := &fakePanel{t: t, frames: make(chan proto.Envelope, 1024), conns: make(chan *websocket.Conn, 4), artifacts: map[string][]byte{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/agent/v1/connect", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		c.SetReadLimit(64 << 20)
		p.conns <- c
		for {
			_, b, err := c.Read(r.Context())
			if err != nil {
				return
			}
			var env proto.Envelope
			if json.Unmarshal(b, &env) == nil {
				p.frames <- env
			}
		}
	})
	mux.HandleFunc("/agent/v1/artifacts/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		b, ok := p.artifacts[r.PathValue("id")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	})
	p.Server = httptest.NewServer(mux)
	t.Cleanup(p.Close)
	return p
}

func (p *fakePanel) send(c *websocket.Conn, id, typ string, v any) {
	b, err := proto.Encode(id, typ, v)
	if err != nil {
		p.t.Fatal(err)
	}
	if err := c.Write(context.Background(), websocket.MessageText, b); err != nil {
		p.t.Fatal(err)
	}
}

// expect waits for a frame matching f, skipping others.
func (p *fakePanel) expect(what string, timeout time.Duration, f func(proto.Envelope) bool) proto.Envelope {
	p.t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case env := <-p.frames:
			if f(env) {
				return env
			}
		case <-deadline:
			p.t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func botState(id, state string) func(proto.Envelope) bool {
	return func(env proto.Envelope) bool {
		if env.Type != proto.TypeBotState {
			return false
		}
		var s proto.BotState
		json.Unmarshal(env.Data, &s)
		return s.BotID == id && s.State == state
	}
}

func reply(id string) func(proto.Envelope) bool {
	return func(env proto.Envelope) bool { return env.ID == id }
}

// ---------- Fake runtime ----------

type fakeRT struct {
	mu       sync.Mutex
	insts    map[string]runtime.Instance
	applies  int
	restarts int
	removed  []string
	events   chan runtime.Event
}

func newFakeRT() *fakeRT {
	return &fakeRT{insts: map[string]runtime.Instance{}, events: make(chan runtime.Event, 8)}
}

func (f *fakeRT) Apply(_ context.Context, s proto.BotSpec) (runtime.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applies++
	in := runtime.Instance{BotID: s.BotID, ContainerID: "c-" + s.BotID, DeployID: s.DeployID, Running: s.Desired == proto.DesiredRunning, HasVolume: true}
	f.insts[s.BotID] = in
	return in, nil
}
func (f *fakeRT) Remove(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.insts, id)
	f.removed = append(f.removed, id)
	return nil
}
func (f *fakeRT) Restart(_ context.Context, id string) (runtime.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restarts++
	in := f.insts[id]
	in.Running = true
	f.insts[id] = in
	return in, nil
}
func (f *fakeRT) List(context.Context) ([]runtime.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []runtime.Instance
	for _, in := range f.insts {
		out = append(out, in)
	}
	return out, nil
}
func (f *fakeRT) Logs(ctx context.Context, id string, tail int, since time.Time, follow bool) (<-chan runtime.LogEntry, error) {
	ch := make(chan runtime.LogEntry, 2)
	if since.IsZero() {
		ch <- runtime.LogEntry{Time: time.Now(), Stream: "stdout", Text: "hello from " + id}
	}
	if !follow {
		close(ch)
		return ch, nil
	}
	go func() { <-ctx.Done(); close(ch) }()
	return ch, nil
}
func (f *fakeRT) Stats(context.Context) ([]proto.BotStats, error) {
	return []proto.BotStats{{BotID: "ag-1", MemoryBytes: 42}}, nil
}
func (f *fakeRT) Events(context.Context) (<-chan runtime.Event, error) { return f.events, nil }
func (f *fakeRT) HostInfo(context.Context) (runtime.HostInfo, error) {
	return runtime.HostInfo{Hostname: "fake", CPUs: 2, MemoryBytes: 1 << 30, CgroupV2: true}, nil
}

func testSpec(id string) proto.BotSpec {
	return proto.BotSpec{
		BotID: id, Name: id, UID: 100500,
		Template: proto.Template{ID: "t", Image: "alpine:3.20", Start: "echo up; exec sleep 3600"},
		Limits:   proto.Limits{MemoryMB: 64, CPUMillicores: 250, DiskMB: 32, Pids: 32},
		Env:      map[string]string{},
		Desired:  proto.DesiredRunning,
	}
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestAgentFakeRuntime(t *testing.T) {
	panel := newFakePanel(t)
	rt := newFakeRT()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := New(Config{PanelURL: panel.URL, Token: token, DataDir: t.TempDir(), Logger: quietLogger()})
	go a.Run(ctx, rt)

	conn := <-panel.conns
	hello := panel.expect("hello", 5*time.Second, func(e proto.Envelope) bool { return e.Type == proto.TypeHello })
	var h proto.Hello
	json.Unmarshal(hello.Data, &h)
	if h.Protocol != proto.Version || h.Hostname != "fake" || h.CPUs != 2 || !h.CgroupV2 {
		t.Fatalf("hello: %+v", h)
	}

	spec := testSpec("ag-1")
	spec.DeployID = "d1"
	spec.ArtifactPath = "/agent/v1/artifacts/d1"
	spec.ArtifactSHA256 = strings.Repeat("a", 64)
	panel.send(conn, "r1", proto.TypeSync, proto.Sync{Bots: []proto.BotSpec{spec}})
	if env := panel.expect("sync reply", 5*time.Second, reply("r1")); env.Type != proto.TypeOK {
		t.Fatalf("sync reply: %s %s", env.Type, env.Data)
	}
	panel.expect("running", 5*time.Second, botState("ag-1", proto.StateRunning))
	panel.expect("stats", 7*time.Second, func(e proto.Envelope) bool { return e.Type == proto.TypeStats })

	// Idempotent apply does not touch the runtime.
	rt.mu.Lock()
	n := rt.applies
	rt.mu.Unlock()
	panel.send(conn, "r2", proto.TypeBotApply, spec)
	panel.expect("apply reply", 5*time.Second, reply("r2"))
	time.Sleep(200 * time.Millisecond)
	rt.mu.Lock()
	same := rt.applies == n
	rt.mu.Unlock()
	if !same {
		t.Fatalf("idempotent apply reached the runtime")
	}

	// Invalid spec is refused synchronously.
	bad := spec
	bad.UID = 0
	panel.send(conn, "r3", proto.TypeBotApply, bad)
	if env := panel.expect("bad apply reply", 5*time.Second, reply("r3")); env.Type != proto.TypeError {
		t.Fatalf("invalid spec accepted")
	}

	// Logs: tail in the reply, system lines streamed.
	panel.send(conn, "r4", proto.TypeLogsSubscribe, proto.LogsSubscribe{BotID: "ag-1", Tail: 10})
	env := panel.expect("logs reply", 5*time.Second, reply("r4"))
	var lb proto.LogBatch
	json.Unmarshal(env.Data, &lb)
	if env.Type != proto.TypeOK || len(lb.Lines) != 1 || lb.Lines[0].Text != "hello from ag-1" {
		t.Fatalf("logs reply: %s %s", env.Type, env.Data)
	}

	// Crash: reported, system lines logged, restarted after ~1 s with restarts=1.
	rt.events <- runtime.Event{BotID: "ag-1", ContainerID: "c-ag-1", ExitCode: 1}
	panel.expect("crashed", 5*time.Second, botState("ag-1", proto.StateCrashed))
	panel.expect("crash log", 5*time.Second, func(e proto.Envelope) bool {
		return e.Type == proto.TypeLog && strings.Contains(string(e.Data), "crashed with exit code 1")
	})
	env = panel.expect("running again", 5*time.Second, botState("ag-1", proto.StateRunning))
	var st proto.BotState
	json.Unmarshal(env.Data, &st)
	if st.Restarts != 1 {
		t.Fatalf("restarts = %d", st.Restarts)
	}

	// Sync without the bot removes it.
	panel.send(conn, "r5", proto.TypeSync, proto.Sync{Bots: []proto.BotSpec{}})
	panel.expect("sync reply", 5*time.Second, reply("r5"))
	deadline := time.Now().Add(5 * time.Second)
	for {
		rt.mu.Lock()
		removed := len(rt.removed)
		rt.mu.Unlock()
		if removed == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("bot not removed by sync")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Reconnect after the panel drops us.
	conn.Close(websocket.StatusGoingAway, "bye")
	select {
	case <-panel.conns:
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not reconnect")
	}
	panel.expect("hello again", 5*time.Second, func(e proto.Envelope) bool { return e.Type == proto.TypeHello })
}

func TestAgentRejectsBadToken(t *testing.T) {
	panel := newFakePanel(t)
	a := New(Config{PanelURL: panel.URL, Token: "wrong", Logger: quietLogger()})
	a.rt = newFakeRT()
	err := a.session(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("want 401, got %v", err)
	}
}

// TestAgentDocker is the same flow against real Docker: the fake panel syncs one bot with a
// deploy, and the agent fetches, installs, runs it and reports state and stats.
func TestAgentDocker(t *testing.T) {
	if os.Getenv("MECHON_TEST_DOCKER") != "1" || os.Geteuid() != 0 {
		t.Skip("set MECHON_TEST_DOCKER=1 (as root, on Linux with Docker) to run")
	}
	panel := newFakePanel(t)
	art := tarGz(t, map[string]string{"run.sh": "echo agent-bot up; exec sleep 3600"})
	panel.artifacts["dk1"] = art
	sum := sha256.Sum256(art)

	dir, err := os.MkdirTemp("/var/lib", "mechon-agent-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := New(Config{PanelURL: panel.URL, Token: token, DataDir: dir, Logger: quietLogger()})
	rt, err := runtime.NewDocker(ctx, runtime.Config{DataDir: dir, PanelURL: panel.URL, NodeToken: token, Hooks: a.Hooks(), Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Remove(context.Background(), "agent-dk")
	go a.Run(ctx, rt)

	conn := <-panel.conns
	panel.expect("hello", 10*time.Second, func(e proto.Envelope) bool { return e.Type == proto.TypeHello })
	spec := testSpec("agent-dk")
	spec.Template.Start = "sh run.sh"
	spec.DeployID, spec.ArtifactPath, spec.ArtifactSHA256 = "dk1", "/agent/v1/artifacts/dk1", hex.EncodeToString(sum[:])
	panel.send(conn, "s1", proto.TypeSync, proto.Sync{Bots: []proto.BotSpec{spec}})
	panel.expect("sync ok", 5*time.Second, func(e proto.Envelope) bool { return e.ID == "s1" && e.Type == proto.TypeOK })
	panel.expect("installing", 30*time.Second, botState("agent-dk", proto.StateInstalling))
	panel.expect("deploy live", 60*time.Second, func(e proto.Envelope) bool {
		return e.Type == proto.TypeDeployProgress && strings.Contains(string(e.Data), `"phase":"live"`)
	})
	panel.expect("running", 30*time.Second, botState("agent-dk", proto.StateRunning))
	env := panel.expect("stats with the bot", 15*time.Second, func(e proto.Envelope) bool {
		return e.Type == proto.TypeStats && strings.Contains(string(e.Data), `"botId":"agent-dk"`)
	})
	t.Logf("stats: %s", env.Data)

	panel.send(conn, "l1", proto.TypeLogsSubscribe, proto.LogsSubscribe{BotID: "agent-dk", Tail: 50})
	env = panel.expect("logs reply", 10*time.Second, reply("l1"))
	if !strings.Contains(string(env.Data), "agent-bot up") {
		t.Fatalf("logs tail: %s", env.Data)
	}
	// A crash (killed from outside) is reported and the bot restarted.
	if out, err := execOut("docker", "kill", "mechon-agent-dk"); err != nil {
		t.Fatalf("kill: %v %s", err, out)
	}
	panel.expect("crashed", 15*time.Second, botState("agent-dk", proto.StateCrashed))
	panel.expect("restart line", 10*time.Second, func(e proto.Envelope) bool {
		return e.Type == proto.TypeLog && strings.Contains(string(e.Data), "restarting in 1s")
	})
	panel.expect("running again", 15*time.Second, botState("agent-dk", proto.StateRunning))

	panel.send(conn, "x1", proto.TypeBotRemove, proto.BotRef{BotID: "agent-dk"})
	panel.expect("remove ok", 5*time.Second, reply("x1"))
	deadline := time.Now().Add(30 * time.Second)
	for {
		list, _ := rt.List(ctx)
		if len(list) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not removed: %+v", list)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func tarGz(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	zw.Close()
	return buf.Bytes()
}
