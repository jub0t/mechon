// Package agent is the node side of Mechon: it keeps a WebSocket connection to the panel,
// reconciles bots onto the specs the panel sends, supervises them, and streams state, stats
// and logs back (spec §4).
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/jub0t/mechon/internal/proto"
	"github.com/jub0t/mechon/internal/runtime"
)

// Version is the agent build version, set with -ldflags "-X .../agent.Version=…".
var Version = "0.1.0-dev"

// Runtime is what the agent needs from the container runtime.
type Runtime interface {
	runtime.Runtime
	HostInfo(ctx context.Context) (runtime.HostInfo, error)
}

type Config struct {
	PanelURL string // http(s)://host[:port]
	Token    string
	DataDir  string // for node disk stats
	Logger   *slog.Logger
}

type Agent struct {
	cfg  Config
	log  *slog.Logger
	rt   Runtime
	host *runtime.HostSampler
	logs *logHub

	ctx context.Context // lifetime of Run; workers live in it

	mu      sync.Mutex
	workers map[string]*worker
	outq    chan []byte // queue of the live connection; nil while disconnected
}

func New(cfg Config) *Agent {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	a := &Agent{
		cfg:     cfg,
		log:     cfg.Logger,
		host:    &runtime.HostSampler{DataDir: cfg.DataDir},
		workers: map[string]*worker{},
	}
	a.logs = newLogHub(a)
	return a
}

// Hooks routes the runtime's deploy output and lifecycle lines to the panel.
func (a *Agent) Hooks() runtime.Hooks {
	return runtime.Hooks{
		DeployProgress: func(p proto.DeployProgress) {
			if p.Phase == proto.PhaseFetching {
				// The hook runs on the bot's worker goroutine (inside Apply).
				a.mu.Lock()
				w := a.workers[p.BotID]
				a.mu.Unlock()
				if w != nil {
					w.deployStarted()
				}
			}
			a.send(proto.TypeDeployProgress, p)
		},
		SystemLog: func(botID, text string) { a.logs.system(botID, text) },
	}
}

// Run connects to the panel and serves it until ctx is cancelled, reconnecting forever.
func (a *Agent) Run(ctx context.Context, rt Runtime) error {
	a.rt = rt
	a.ctx = ctx
	events, err := rt.Events(ctx)
	if err != nil {
		return err
	}
	go func() {
		for ev := range events {
			a.withWorker(ev.BotID, func(w *worker) { w.exited(ev) })
		}
	}()
	go a.statsLoop(ctx)
	go a.logs.flushLoop(ctx)
	if fw, ok := rt.(interface{ EnsureFirewall() error }); ok {
		go func() {
			t := time.NewTicker(time.Minute)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if err := fw.EnsureFirewall(); err != nil {
						a.log.Error("firewall", "err", err)
					}
				}
			}
		}()
	}

	backoff := time.Second
	for {
		start := time.Now()
		err := a.session(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Since(start) > time.Minute {
			backoff = time.Second
		}
		wait := backoff/2 + rand.N(backoff/2+1) // jitter in [backoff/2, backoff]
		a.log.Warn("disconnected from panel", "err", err, "retry_in", wait.Round(time.Millisecond))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func wsURL(panel string) (string, error) {
	u, err := url.Parse(panel)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("panel URL must be http(s), got %q", panel)
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/agent/v1/connect"
	return u.String(), nil
}

// session runs one connection: dial, hello, then serve until it breaks.
func (a *Agent) session(ctx context.Context) error {
	u, err := wsURL(a.cfg.PanelURL)
	if err != nil {
		return err
	}
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	conn, resp, err := websocket.Dial(dctx, u, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + a.cfg.Token}},
	})
	cancel()
	if err != nil {
		if resp != nil {
			return fmt.Errorf("dial %s: HTTP %d: %w", u, resp.StatusCode, err)
		}
		return fmt.Errorf("dial %s: %w", u, err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(64 << 20)

	ctx, cancel = context.WithCancel(ctx)
	defer cancel()

	hello, err := a.hello(ctx)
	if err != nil {
		return fmt.Errorf("hello: %w", err)
	}
	b, err := proto.Encode("", proto.TypeHello, hello)
	if err != nil {
		return err
	}
	if err := writeTimeout(ctx, conn, b); err != nil {
		return err
	}
	a.log.Info("connected to panel", "url", u, "bots", len(hello.Bots))

	q := make(chan []byte, 4096)
	a.mu.Lock()
	a.outq = q
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		if a.outq == q {
			a.outq = nil
		}
		a.mu.Unlock()
		a.logs.closeAll()
	}()

	errc := make(chan error, 3)
	go func() { // writer
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-q:
				if err := writeTimeout(ctx, conn, m); err != nil {
					errc <- fmt.Errorf("write: %w", err)
					return
				}
			}
		}
	}()
	go func() { // our own liveness check; the panel's pings are answered inside Read
		t := time.NewTicker(proto.PingInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				pctx, pcancel := context.WithTimeout(ctx, proto.DeadAfter-proto.PingInterval)
				err := conn.Ping(pctx)
				pcancel()
				if err != nil {
					errc <- fmt.Errorf("ping: %w", err)
					return
				}
			}
		}
	}()
	go func() { // reader
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				errc <- fmt.Errorf("read: %w", err)
				return
			}
			if typ != websocket.MessageText {
				continue
			}
			var env proto.Envelope
			if err := json.Unmarshal(data, &env); err != nil {
				a.log.Warn("bad frame from panel", "err", err)
				continue
			}
			a.handle(env)
		}
	}()
	err = <-errc
	conn.Close(websocket.StatusGoingAway, "")
	return err
}

func writeTimeout(ctx context.Context, conn *websocket.Conn, b []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, b)
}

// send queues a frame on the live connection. While disconnected, frames are dropped: hello
// and the panel's sync rebuild the full picture on reconnect.
func (a *Agent) send(typ string, v any) { a.sendID("", typ, v) }

func (a *Agent) sendID(id, typ string, v any) {
	b, err := proto.Encode(id, typ, v)
	if err != nil {
		a.log.Error("encode", "type", typ, "err", err)
		return
	}
	a.mu.Lock()
	q := a.outq
	a.mu.Unlock()
	if q == nil {
		return
	}
	select {
	case q <- b:
	default:
		a.log.Warn("outbound queue full, dropping frame", "type", typ)
	}
}

func (a *Agent) reply(id string, err error, data any) {
	if id == "" {
		return
	}
	if err != nil {
		a.sendID(id, proto.TypeError, proto.ErrorReply{Message: err.Error()})
		return
	}
	a.sendID(id, proto.TypeOK, data)
}

func (a *Agent) hello(ctx context.Context) (proto.Hello, error) {
	h := proto.Hello{Protocol: proto.Version, AgentVersion: Version, Bots: []proto.BotState{}}
	info, err := a.rt.HostInfo(ctx)
	if err != nil {
		return h, err
	}
	h.Hostname, h.Kernel, h.DockerVersion = info.Hostname, info.Kernel, info.DockerVersion
	h.CgroupV2, h.Runsc, h.CPUs = info.CgroupV2, info.Runsc, info.CPUs
	h.MemoryBytes, h.DiskBytes = info.MemoryBytes, info.DiskBytes

	insts, err := a.rt.List(ctx)
	if err != nil {
		return h, err
	}
	for _, in := range insts {
		// Bots we have no worker for yet (agent just started) get one primed with what the
		// runtime sees, so crashes are attributed and "installing" is reported correctly.
		a.mu.Lock()
		w := a.workers[in.BotID]
		if w == nil {
			w = newWorker(a, in.BotID)
			w.container = in.ContainerID
			w.state, w.hasState = stateFromInstance(in, ""), true
			w.state.At = time.Now().UTC()
			a.workers[in.BotID] = w
			go w.run(a.ctx)
		}
		a.mu.Unlock()
		if st, ok := w.current(); ok {
			h.Bots = append(h.Bots, st)
		} else {
			h.Bots = append(h.Bots, stateFromInstance(in, ""))
		}
	}
	return h, nil
}

// stateFromInstance derives an observed state when no worker has an opinion yet.
func stateFromInstance(in runtime.Instance, desired string) proto.BotState {
	st := proto.BotState{BotID: in.BotID, DeployID: in.DeployID, At: time.Now().UTC()}
	switch {
	case in.ContainerID == "":
		st.State = proto.StatePending
	case in.Running:
		st.State = proto.StateRunning
	case desired == proto.DesiredStopped || (desired == "" && in.ExitCode == 0 && !in.OOMKilled):
		st.State = proto.StateStopped
	default:
		st.State = proto.StateCrashed
		code := in.ExitCode
		st.ExitCode = &code
		st.OOMKilled = in.OOMKilled
	}
	return st
}

// ---------- Requests ----------

func (a *Agent) handle(env proto.Envelope) {
	switch env.Type {
	case proto.TypeSync:
		var s proto.Sync
		if err := json.Unmarshal(env.Data, &s); err != nil {
			a.reply(env.ID, fmt.Errorf("bad sync: %w", err), nil)
			return
		}
		a.reply(env.ID, a.sync(s), nil)
	case proto.TypeBotApply:
		var spec proto.BotSpec
		if err := json.Unmarshal(env.Data, &spec); err != nil {
			a.reply(env.ID, fmt.Errorf("bad spec: %w", err), nil)
			return
		}
		if err := runtime.ValidateSpec(spec); err != nil {
			a.reply(env.ID, err, nil)
			return
		}
		a.withWorker(spec.BotID, func(w *worker) { w.apply(spec) })
		a.reply(env.ID, nil, nil)
	case proto.TypeBotRemove, proto.TypeBotRestart, proto.TypeLogsUnsubscribe:
		var ref proto.BotRef
		if err := json.Unmarshal(env.Data, &ref); err != nil || !validID(ref.BotID) {
			a.reply(env.ID, fmt.Errorf("bad bot ref"), nil)
			return
		}
		switch env.Type {
		case proto.TypeBotRemove:
			a.withWorker(ref.BotID, func(w *worker) { w.remove() })
		case proto.TypeBotRestart:
			a.withWorker(ref.BotID, func(w *worker) { w.restart() })
		case proto.TypeLogsUnsubscribe:
			a.logs.unsubscribe(ref.BotID)
		}
		a.reply(env.ID, nil, nil)
	case proto.TypeLogsSubscribe:
		var sub proto.LogsSubscribe
		if err := json.Unmarshal(env.Data, &sub); err != nil || !validID(sub.BotID) {
			a.reply(env.ID, fmt.Errorf("bad logs.subscribe"), nil)
			return
		}
		go a.logs.subscribe(env.ID, sub)
	default:
		a.reply(env.ID, fmt.Errorf("unknown request type %q", env.Type), nil)
	}
}

func validID(id string) bool { return runtime.ValidBotID(id) }

// sync applies every valid spec and removes every bot that is not in the set. Invalid specs
// are reported in the error reply and left alone (neither applied nor removed).
func (a *Agent) sync(s proto.Sync) error {
	keep := map[string]bool{}
	var bad []error
	for _, spec := range s.Bots {
		if validID(spec.BotID) {
			keep[spec.BotID] = true
		}
		if err := runtime.ValidateSpec(spec); err != nil {
			bad = append(bad, fmt.Errorf("bot %q: %w", spec.BotID, err))
			continue
		}
		a.withWorker(spec.BotID, func(w *worker) { w.apply(spec) })
	}
	// Removal needs the runtime's view (a Docker call): do it off the read loop.
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, time.Minute)
		defer cancel()
		ids := map[string]bool{}
		insts, err := a.rt.List(ctx)
		if err != nil {
			a.log.Error("sync: list", "err", err)
			return
		}
		for _, in := range insts {
			ids[in.BotID] = true
		}
		a.mu.Lock()
		for id := range a.workers {
			ids[id] = true
		}
		a.mu.Unlock()
		for id := range ids {
			if !keep[id] {
				a.log.Info("sync: removing bot not in the panel's set", "bot", id)
				a.withWorker(id, func(w *worker) { w.remove() })
			}
		}
	}()
	return errors.Join(bad...)
}

// withWorker calls fn with the bot's worker, starting one if needed. fn runs under the agent
// lock so a worker cannot retire between being looked up and receiving the request; it must
// only record intent (the worker's request methods do exactly that).
func (a *Agent) withWorker(botID string, fn func(*worker)) {
	if !validID(botID) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	w := a.workers[botID]
	if w == nil {
		w = newWorker(a, botID)
		a.workers[botID] = w
		go w.run(a.ctx)
	}
	fn(w)
}

// retire removes w from the worker map if it has nothing left to do. Called by the worker
// after a removal; returns false if new work arrived meanwhile.
func (a *Agent) retire(w *worker) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if w.hasWork() {
		return false
	}
	if a.workers[w.id] == w {
		delete(a.workers, w.id)
	}
	return true
}

// ---------- Stats ----------

func (a *Agent) statsLoop(ctx context.Context) {
	t := time.NewTicker(proto.StatsInterval)
	defer t.Stop()
	a.host.Sample() // prime CPU deltas
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		sctx, cancel := context.WithTimeout(ctx, proto.StatsInterval)
		bots, err := a.rt.Stats(sctx)
		cancel()
		if err != nil {
			a.log.Warn("stats", "err", err)
		}
		if bots == nil {
			bots = []proto.BotStats{}
		}
		a.send(proto.TypeStats, proto.Stats{At: time.Now().UTC(), Node: a.host.Sample(), Bots: bots})
	}
}
