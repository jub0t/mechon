package agent

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/jub0t/mechon/internal/proto"
	"github.com/jub0t/mechon/internal/runtime"
)

const (
	backoffMin   = time.Second
	backoffMax   = 5 * time.Minute
	healthyAfter = 10 * time.Minute // a bot up this long gets its backoff reset
	opTimeout    = 30 * time.Minute // upper bound for one Apply (download + install + start)
)

// worker owns one bot. Every runtime call for the bot happens on its goroutine, so operations
// on one bot are serialised while bots never wait on each other. Requests only record intent
// and kick the goroutine; the newest spec wins.
type worker struct {
	a  *Agent
	id string

	mu         sync.Mutex
	pendApply  *proto.BotSpec
	pendRemove bool
	pendRestrt bool
	pendExits  []runtime.Event
	cancelOp   context.CancelFunc // cancels the in-flight op (superseding deploy, removal)
	opDeploy   string             // DeployID of the in-flight apply
	state      proto.BotState
	hasState   bool
	uid        int // UID of the newest spec received (0 before the first); for the file manager
	kick       chan struct{}

	// Owned by run.
	spec      *proto.BotSpec // desired spec, as last received
	applied   *proto.BotSpec // last spec that applied without error
	container string
	backoff   time.Duration
	startedAt time.Time
	retry     *time.Timer
	restarts  int
}

func newWorker(a *Agent, id string) *worker {
	return &worker{a: a, id: id, kick: make(chan struct{}, 1)}
}

func (w *worker) poke() {
	select {
	case w.kick <- struct{}{}:
	default:
	}
}

func (w *worker) apply(spec proto.BotSpec) {
	w.mu.Lock()
	w.pendApply = &spec
	w.pendRemove = false
	w.uid = spec.UID
	// A new deploy supersedes one still being fetched or installed.
	if w.cancelOp != nil && w.opDeploy != "" && spec.DeployID != w.opDeploy {
		w.cancelOp()
	}
	w.mu.Unlock()
	w.poke()
}

func (w *worker) remove() {
	w.mu.Lock()
	w.pendApply = nil
	w.pendRestrt = false
	w.pendRemove = true
	w.uid = 0
	if w.cancelOp != nil {
		w.cancelOp()
	}
	w.mu.Unlock()
	w.poke()
}

func (w *worker) restart() {
	w.mu.Lock()
	w.pendRestrt = true
	w.mu.Unlock()
	w.poke()
}

func (w *worker) exited(ev runtime.Event) {
	w.mu.Lock()
	w.pendExits = append(w.pendExits, ev)
	w.mu.Unlock()
	w.poke()
}

// deployStarted is called on the worker goroutine (from its Apply, via the runtime hook) when a deploy
// begins fetching. A bot that is not serving an older version meanwhile shows as installing.
func (w *worker) deployStarted() {
	prev, _ := w.current()
	if prev.State == proto.StateRunning {
		return
	}
	w.setState(proto.BotState{State: proto.StateInstalling, DeployID: prev.DeployID})
}

func (w *worker) hasWork() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.pendApply != nil || w.pendRemove || w.pendRestrt || len(w.pendExits) > 0
}

// specUID returns the bot's UID from its newest spec, or 0 if no spec has been received.
func (w *worker) specUID() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.uid
}

// current returns the last reported state, if any.
func (w *worker) current() (proto.BotState, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state, w.hasState
}

func (w *worker) run(ctx context.Context) {
	for {
		var retryC <-chan time.Time
		if w.retry != nil {
			retryC = w.retry.C
		}
		select {
		case <-ctx.Done():
			return
		case <-w.kick:
		case <-retryC:
			w.retry = nil
			w.crashRestart(ctx)
		}
		for {
			w.mu.Lock()
			apply, remove, restart, exits := w.pendApply, w.pendRemove, w.pendRestrt, w.pendExits
			w.pendApply, w.pendRemove, w.pendRestrt, w.pendExits = nil, false, false, nil
			w.mu.Unlock()
			if apply == nil && !remove && !restart && len(exits) == 0 {
				break
			}
			for _, ev := range exits {
				w.handleExit(ev)
			}
			if remove {
				w.doRemove(ctx)
				if w.a.retire(w) {
					return
				}
				continue
			}
			if apply != nil {
				w.doApply(ctx, *apply)
			}
			if restart {
				w.doRestart(ctx)
			}
		}
	}
}

func (w *worker) opContext(ctx context.Context, deployID string) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	w.mu.Lock()
	w.cancelOp, w.opDeploy = cancel, deployID
	w.mu.Unlock()
	return ctx, func() {
		w.mu.Lock()
		w.cancelOp, w.opDeploy = nil, ""
		w.mu.Unlock()
		cancel()
	}
}

func (w *worker) stopRetry() {
	if w.retry != nil {
		w.retry.Stop()
		w.retry = nil
	}
}

func (w *worker) doApply(ctx context.Context, spec proto.BotSpec) {
	w.spec = &spec
	if w.applied != nil && reflect.DeepEqual(*w.applied, spec) {
		return // idempotent: nothing changed since the last successful apply
	}
	w.stopRetry()
	octx, done := w.opContext(ctx, spec.DeployID)
	inst, err := w.a.rt.Apply(octx, spec)
	done()
	if err == nil {
		w.applied = &spec
	} else {
		w.applied = nil
		w.a.log.Error("apply", "bot", w.id, "err", err)
	}
	w.observe(inst, spec.Desired, err)
}

// observe turns a runtime Instance into the reported state.
func (w *worker) observe(inst runtime.Instance, desired string, err error) {
	prev, _ := w.current()
	if inst.ContainerID != w.container {
		w.container = inst.ContainerID
	}
	st := stateFromInstance(inst, desired)
	if inst.ContainerID != "" && !inst.Running && desired == proto.DesiredRunning && err == nil {
		// Started, then exited before we looked: the exit event will follow.
		st.State = proto.StateCrashed
	}
	if st.State == proto.StateRunning && prev.State != proto.StateRunning {
		w.startedAt = time.Now()
	}
	if st.State == proto.StateRunning || st.State == proto.StateStopped || st.State == proto.StatePending {
		st.ExitCode, st.OOMKilled = nil, false
	}
	if err != nil {
		st.Error = err.Error()
	}
	w.setState(st)
}

func (w *worker) setState(st proto.BotState) {
	st.BotID = w.id
	st.Restarts = w.restarts
	st.At = time.Now().UTC()
	w.mu.Lock()
	prev, had := w.state, w.hasState
	w.state, w.hasState = st, true
	w.mu.Unlock()
	if had && sameState(prev, st) {
		return
	}
	w.a.send(proto.TypeBotState, st)
}

func sameState(a, b proto.BotState) bool {
	ea, eb := -1000, -1000
	if a.ExitCode != nil {
		ea = *a.ExitCode
	}
	if b.ExitCode != nil {
		eb = *b.ExitCode
	}
	return a.State == b.State && a.DeployID == b.DeployID && ea == eb &&
		a.OOMKilled == b.OOMKilled && a.Restarts == b.Restarts && a.Error == b.Error
}

func (w *worker) handleExit(ev runtime.Event) {
	if w.container != "" && ev.ContainerID != w.container {
		return // an old container of this bot
	}
	prev, _ := w.current()
	code := ev.ExitCode
	st := proto.BotState{DeployID: prev.DeployID, ExitCode: &code, OOMKilled: ev.OOMKilled}
	if w.spec != nil && w.spec.Desired == proto.DesiredStopped {
		st.State = proto.StateStopped
		w.setState(st)
		return
	}
	st.State = proto.StateCrashed
	w.setState(st)
	if ev.OOMKilled {
		limit := ""
		if w.spec != nil {
			limit = fmt.Sprintf(" (limit %d MB)", w.spec.Limits.MemoryMB)
		}
		w.a.logs.system(w.id, "killed: out of memory"+limit)
	} else {
		w.a.logs.system(w.id, fmt.Sprintf("crashed with exit code %d", code))
	}
	if w.spec == nil {
		return // before the first sync: the panel's spec decides whether it runs again
	}
	if w.backoff == 0 || time.Since(w.startedAt) >= healthyAfter {
		w.backoff = backoffMin
	} else {
		w.backoff = min(w.backoff*2, backoffMax)
	}
	w.a.logs.system(w.id, fmt.Sprintf("restarting in %s", w.backoff))
	w.stopRetry()
	w.retry = time.NewTimer(w.backoff)
}

func (w *worker) crashRestart(ctx context.Context) {
	if w.spec == nil || w.spec.Desired != proto.DesiredRunning {
		return
	}
	if st, _ := w.current(); st.State != proto.StateCrashed {
		return
	}
	octx, done := w.opContext(ctx, "")
	inst, err := w.a.rt.Restart(octx, w.id)
	done()
	w.restarts++
	if err != nil {
		w.a.log.Error("restart", "bot", w.id, "err", err)
		// Treat a failed start like another crash.
		w.observe(inst, proto.DesiredRunning, err)
		w.backoff = min(max(w.backoff*2, backoffMin), backoffMax)
		w.retry = time.NewTimer(w.backoff)
		return
	}
	w.observe(inst, proto.DesiredRunning, nil)
}

func (w *worker) doRestart(ctx context.Context) {
	w.stopRetry()
	octx, done := w.opContext(ctx, "")
	inst, err := w.a.rt.Restart(octx, w.id)
	done()
	if err != nil {
		w.a.log.Error("restart", "bot", w.id, "err", err)
	}
	w.backoff = 0
	w.startedAt = time.Now()
	desired := proto.DesiredRunning
	if w.spec != nil {
		desired = w.spec.Desired
	}
	// A manual restart of a stopped bot runs it until the next apply says otherwise.
	if err == nil {
		desired = proto.DesiredRunning
	}
	w.observe(inst, desired, err)
}

func (w *worker) doRemove(ctx context.Context) {
	w.stopRetry()
	w.a.logs.unsubscribe(w.id)
	octx, done := w.opContext(ctx, "")
	err := w.a.rt.Remove(octx, w.id)
	done()
	w.spec, w.applied, w.container = nil, nil, ""
	if err != nil {
		w.a.log.Error("remove", "bot", w.id, "err", err)
		w.setState(proto.BotState{State: proto.StateStopped, Error: "remove failed: " + err.Error()})
		return
	}
	w.a.log.Info("removed bot", "bot", w.id)
}
