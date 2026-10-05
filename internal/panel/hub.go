package panel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jub0t/mechon/internal/auth"
	"github.com/jub0t/mechon/internal/db"
	"github.com/jub0t/mechon/internal/proto"
	"github.com/jub0t/mechon/internal/templates"
)

// Hub owns the WebSocket connections to node agents. It turns database state into bot specs and
// sends them down, and turns what agents report (state, stats, logs, deploy progress) into
// database updates and live events for browsers watching a bot.
type Hub struct {
	s *Server

	mu        sync.Mutex
	conns     map[uuid.UUID]*agentConn
	nodeStats map[uuid.UUID]nodeLive
	botStats  map[uuid.UUID]proto.BotStats
	minute    map[uuid.UUID]time.Time // last metrics bucket written per bot
	watchers  map[uuid.UUID]*botWatch // bots someone is watching in the browser
}

type nodeLive struct {
	Hello       proto.Hello
	Stats       proto.NodeStats
	ConnectedAt time.Time
	StatsAt     time.Time
}

// botWatch is the browser side of one bot: its live subscribers and a buffer of recent log lines.
type botWatch struct {
	nodeID uuid.UUID
	subs   map[chan streamEvent]struct{}
	logs   []proto.LogLine
}

type streamEvent struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

const logBuffer = 500

func newHub(s *Server) *Hub {
	return &Hub{
		s:         s,
		conns:     map[uuid.UUID]*agentConn{},
		nodeStats: map[uuid.UUID]nodeLive{},
		botStats:  map[uuid.UUID]proto.BotStats{},
		minute:    map[uuid.UUID]time.Time{},
		watchers:  map[uuid.UUID]*botWatch{},
	}
}

// ---------- Agent connections ----------

type agentConn struct {
	nodeID uuid.UUID
	ws     *websocket.Conn

	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[string]chan proto.Envelope
}

func (c *agentConn) send(ctx context.Context, id, typ string, v any) error {
	b, err := proto.Encode(id, typ, v)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.ws.Write(ctx, websocket.MessageText, b)
}

// request sends a request and waits for the agent's ok/error reply.
func (c *agentConn) request(ctx context.Context, typ string, v any) (json.RawMessage, error) {
	id := uuid.NewString()
	ch := make(chan proto.Envelope, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()
	if err := c.send(ctx, id, typ, v); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case env := <-ch:
		if env.Type == proto.TypeError {
			var e proto.ErrorReply
			_ = json.Unmarshal(env.Data, &e)
			return nil, fmt.Errorf("agent: %s", e.Message)
		}
		return env.Data, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("agent did not answer %s: %w", typ, ctx.Err())
	}
}

func (c *agentConn) reply(env proto.Envelope) bool {
	c.mu.Lock()
	ch, ok := c.pending[env.ID]
	c.mu.Unlock()
	if ok {
		ch <- env
	}
	return ok
}

func (h *Hub) conn(nodeID uuid.UUID) *agentConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conns[nodeID]
}

func (h *Hub) nodeConnByString(nodeID string) (*agentConn, error) {
	id, err := uuid.Parse(nodeID)
	if err != nil {
		return nil, errNodeOffline
	}
	c := h.conn(id)
	if c == nil {
		return nil, errNodeOffline
	}
	return c, nil
}

func (h *Hub) online(nodeID uuid.UUID) (nodeLive, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.conns[nodeID]
	return h.nodeStats[nodeID], ok
}

// serveAgent is GET /agent/v1/connect. The agent authenticates with its node token.
func (h *Hub) serveAgent(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		http.Error(w, "missing node token", http.StatusUnauthorized)
		return
	}
	node, err := h.s.q.GetNodeByTokenHash(r.Context(), auth.HashToken(strings.TrimSpace(token)))
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "unknown node token", http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(16 << 20)
	c := &agentConn{nodeID: node.ID, ws: ws, pending: map[string]chan proto.Envelope{}}
	log := slog.With("node", node.Name)

	// The agent speaks first.
	helloCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	_, raw, err := ws.Read(helloCtx)
	cancel()
	var env proto.Envelope
	var hello proto.Hello
	if err == nil {
		err = json.Unmarshal(raw, &env)
	}
	if err == nil && env.Type != proto.TypeHello {
		err = fmt.Errorf("first frame was %q, want hello", env.Type)
	}
	if err == nil {
		err = json.Unmarshal(env.Data, &hello)
	}
	if err == nil && hello.Protocol != proto.Version {
		err = fmt.Errorf("agent speaks protocol %d, panel speaks %d; upgrade the agent", hello.Protocol, proto.Version)
	}
	if err != nil {
		log.Warn("agent rejected", "err", err)
		ws.Close(websocket.StatusPolicyViolation, truncate(err.Error(), 120))
		return
	}

	ctx, stop := context.WithCancel(context.WithoutCancel(r.Context()))
	defer stop()
	info, _ := json.Marshal(hello)
	if err := h.s.q.UpdateNodeHello(ctx, db.UpdateNodeHelloParams{ID: node.ID, AgentVersion: hello.AgentVersion, Info: info, Runsc: hello.Runsc}); err != nil {
		log.Error("node hello", "err", err)
	}

	h.mu.Lock()
	if old := h.conns[node.ID]; old != nil {
		old.ws.Close(websocket.StatusPolicyViolation, "replaced by a newer connection")
	}
	h.conns[node.ID] = c
	h.nodeStats[node.ID] = nodeLive{Hello: hello, ConnectedAt: time.Now()}
	h.mu.Unlock()
	log.Info("agent connected", "version", hello.AgentVersion, "bots", len(hello.Bots))
	h.s.emit(ctx, "node.online", map[string]any{"id": node.ID, "name": node.Name, "agentVersion": hello.AgentVersion})

	for _, st := range hello.Bots {
		h.onBotState(ctx, node.ID, st)
	}
	go h.syncNode(ctx, c)
	go h.pingLoop(ctx, c, stop)

	err = h.readLoop(ctx, c)
	log.Info("agent disconnected", "err", err)

	h.mu.Lock()
	if h.conns[node.ID] == c {
		delete(h.conns, node.ID)
		delete(h.nodeStats, node.ID)
		h.mu.Unlock()
		if err := h.s.q.MarkNodeBotsUnknown(context.WithoutCancel(ctx), node.ID); err != nil {
			log.Error("mark bots unknown", "err", err)
		}
		h.s.emit(ctx, "node.offline", map[string]any{"id": node.ID, "name": node.Name})
	} else {
		h.mu.Unlock()
	}
	ws.CloseNow()
}

func (h *Hub) pingLoop(ctx context.Context, c *agentConn, stop func()) {
	t := time.NewTicker(proto.PingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, proto.DeadAfter-proto.PingInterval)
			err := c.ws.Ping(pctx)
			cancel()
			if err != nil {
				c.ws.Close(websocket.StatusGoingAway, "ping timeout")
				stop()
				return
			}
		}
	}
}

func (h *Hub) readLoop(ctx context.Context, c *agentConn) error {
	lastTouch := time.Time{}
	for {
		_, raw, err := c.ws.Read(ctx)
		if err != nil {
			return err
		}
		var env proto.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			slog.Warn("agent sent bad frame", "node", c.nodeID, "err", err)
			continue
		}
		if env.ID != "" && (env.Type == proto.TypeOK || env.Type == proto.TypeError) {
			c.reply(env)
			continue
		}
		switch env.Type {
		case proto.TypeBotState:
			var st proto.BotState
			if json.Unmarshal(env.Data, &st) == nil {
				h.onBotState(ctx, c.nodeID, st)
			}
		case proto.TypeStats:
			var st proto.Stats
			if json.Unmarshal(env.Data, &st) == nil {
				h.onStats(ctx, c.nodeID, st)
				if time.Since(lastTouch) > 20*time.Second {
					lastTouch = time.Now()
					if err := h.s.q.TouchNode(ctx, c.nodeID); err != nil {
						slog.Warn("touch node", "err", err)
					}
				}
			}
		case proto.TypeLog:
			var b proto.LogBatch
			if json.Unmarshal(env.Data, &b) == nil {
				h.onLogs(b)
			}
		case proto.TypeDeployProgress:
			var p proto.DeployProgress
			if json.Unmarshal(env.Data, &p) == nil {
				h.onDeployProgress(ctx, p)
			}
		}
	}
}

// ---------- From agents ----------

var stateMap = map[string]db.BotObservedState{
	proto.StatePending:    db.BotObservedStatePending,
	proto.StateInstalling: db.BotObservedStateInstalling,
	proto.StateRunning:    db.BotObservedStateRunning,
	proto.StateStopped:    db.BotObservedStateStopped,
	proto.StateCrashed:    db.BotObservedStateCrashed,
}

func (h *Hub) onBotState(ctx context.Context, nodeID uuid.UUID, st proto.BotState) {
	id, err := uuid.Parse(st.BotID)
	if err != nil {
		return
	}
	state, ok := stateMap[st.State]
	if !ok {
		state = db.BotObservedStateUnknown
	}
	var exit *int32
	if st.ExitCode != nil {
		e := int32(*st.ExitCode)
		exit = &e
	}
	errText := st.Error
	if st.OOMKilled && errText == "" {
		errText = "Killed: the bot ran out of memory."
	}
	if err := h.s.q.SetBotObserved(ctx, db.SetBotObservedParams{ID: id, ObservedState: state, ObservedError: errText, ExitCode: exit, RestartCount: int32(st.Restarts)}); err != nil {
		slog.Error("bot state", "bot", id, "err", err)
	}
	if st.State == proto.StateCrashed {
		if row, err := h.s.q.GetBot(ctx, id); err == nil {
			ev := botEvent(row)
			ev["exitCode"], ev["oomKilled"], ev["restarts"], ev["error"] = st.ExitCode, st.OOMKilled, st.Restarts, errText
			h.s.emit(ctx, "bot.crashed", ev)
		}
	}
	h.publish(id, streamEvent{Type: "state", Data: map[string]any{
		"state": state, "error": errText, "exitCode": exit, "restarts": st.Restarts, "oomKilled": st.OOMKilled, "at": st.At,
	}})
}

func (h *Hub) onStats(ctx context.Context, nodeID uuid.UUID, st proto.Stats) {
	minute := st.At.Truncate(time.Minute)
	var flush []proto.BotStats
	h.mu.Lock()
	live := h.nodeStats[nodeID]
	live.Stats, live.StatsAt = st.Node, st.At
	h.nodeStats[nodeID] = live
	for _, b := range st.Bots {
		id, err := uuid.Parse(b.BotID)
		if err != nil {
			continue
		}
		h.botStats[id] = b
		// One metrics row per bot per minute: the first sample of each minute.
		if h.minute[id] != minute {
			h.minute[id] = minute
			flush = append(flush, b)
		}
	}
	h.mu.Unlock()

	for _, b := range st.Bots {
		if id, err := uuid.Parse(b.BotID); err == nil {
			h.publish(id, streamEvent{Type: "stats", Data: b})
		}
	}
	for _, b := range flush {
		id, _ := uuid.Parse(b.BotID)
		if err := h.s.q.UpsertBotMetric(ctx, db.UpsertBotMetricParams{BotID: id, Ts: minute, CpuPct: float32(b.CPUPercent),
			MemoryBytes: b.MemoryBytes, DiskBytes: b.DiskBytes, NetRx: b.NetRxBytes, NetTx: b.NetTxBytes}); err != nil {
			// The bot may have been deleted between the sample and the write.
			slog.Debug("metrics", "bot", id, "err", err)
		}
	}
}

func (h *Hub) onLogs(b proto.LogBatch) {
	id, err := uuid.Parse(b.BotID)
	if err != nil || len(b.Lines) == 0 {
		return
	}
	h.mu.Lock()
	if w := h.watchers[id]; w != nil {
		w.logs = append(w.logs, b.Lines...)
		if over := len(w.logs) - logBuffer; over > 0 {
			w.logs = append([]proto.LogLine(nil), w.logs[over:]...)
		}
	}
	h.mu.Unlock()
	h.publish(id, streamEvent{Type: "logs", Data: b.Lines})
}

var phaseStatus = map[string]db.DeployStatus{
	proto.PhaseFetching:   db.DeployStatusFetching,
	proto.PhaseInstalling: db.DeployStatusInstalling,
	proto.PhaseLive:       db.DeployStatusLive,
	proto.PhaseFailed:     db.DeployStatusFailed,
}

func (h *Hub) onDeployProgress(ctx context.Context, p proto.DeployProgress) {
	id, err := uuid.Parse(p.DeployID)
	if err != nil {
		return
	}
	botID, _ := uuid.Parse(p.BotID)
	if len(p.Lines) > 0 {
		var sb strings.Builder
		for _, l := range p.Lines {
			sb.WriteString(l.Text)
			sb.WriteByte('\n')
		}
		if err := h.s.q.AppendDeployLog(ctx, db.AppendDeployLogParams{ID: id, Log: sb.String()}); err != nil {
			slog.Warn("deploy log", "deploy", id, "err", err)
		}
	}
	if status, ok := phaseStatus[p.Phase]; ok {
		if err := h.s.q.SetDeployStatus(ctx, db.SetDeployStatusParams{ID: id, Status: status, Error: p.Error}); err != nil {
			slog.Error("deploy status", "deploy", id, "err", err)
		}
		if status == db.DeployStatusLive || status == db.DeployStatusFailed {
			if d, err := h.s.q.GetDeploy(ctx, id); err == nil {
				ev := map[string]any{"id": d.ID, "number": d.Number, "botId": d.BotID, "source": d.Source, "sha256": d.ArtifactSha256, "error": p.Error}
				if d.GitCommit != "" {
					ev["git"] = map[string]any{"url": d.GitUrl, "ref": d.GitRef, "commit": d.GitCommit}
				}
				h.s.emit(ctx, "deploy."+string(status), ev)
			}
		}
		switch status {
		case db.DeployStatusLive:
			_ = h.s.q.SupersedeLiveDeploys(ctx, db.SupersedeLiveDeploysParams{BotID: botID, ID: id})
		case db.DeployStatusFailed:
			// The agent keeps the previous version running. Point the bot back at it so the desired
			// state matches what is actually live, and the failed deploy is not retried on reconnect.
			var prev *uuid.UUID
			if last, err := h.s.q.LastLiveDeploy(ctx, botID); err == nil {
				prev = &last
			}
			if err := h.s.q.SetBotCurrentDeploy(ctx, db.SetBotCurrentDeployParams{ID: botID, CurrentDeployID: prev}); err == nil {
				h.pushBot(ctx, botID)
			}
		}
	}
	h.publish(botID, streamEvent{Type: "deploy", Data: p})
}

// ---------- To agents ----------

type specRow struct {
	Bot            db.Bot
	PidsMax        int32
	Hardened       bool
	SubStatus      db.SubscriptionStatus
	OwnerSuspended bool
	ArtifactSHA256 *string
}

func (h *Hub) buildSpec(ctx context.Context, r specRow) (proto.BotSpec, error) {
	b := r.Bot
	tpl, ok := templates.Get(b.Template)
	if !ok {
		return proto.BotSpec{}, fmt.Errorf("bot %s uses unknown template %q", b.ID, b.Template)
	}
	env, err := h.s.decryptEnv(ctx, b.ID)
	if err != nil {
		return proto.BotSpec{}, err
	}
	desired := proto.DesiredRunning
	if b.DesiredState == db.BotDesiredStateStopped || r.SubStatus != db.SubscriptionStatusActive || r.OwnerSuspended {
		desired = proto.DesiredStopped
	}
	spec := proto.BotSpec{
		BotID:    b.ID.String(),
		Name:     b.Name,
		UID:      100000 + int(b.UidSeq),
		Template: tpl.Proto(),
		Limits:   proto.Limits{MemoryMB: int(b.MemoryMb), CPUMillicores: int(b.CpuMillicores), DiskMB: int(b.DiskMb), Pids: int(r.PidsMax)},
		Hardened: r.Hardened,
		Env:      env,
		Desired:  desired,
	}
	if b.CurrentDeployID != nil && r.ArtifactSHA256 != nil {
		spec.DeployID = b.CurrentDeployID.String()
		spec.ArtifactPath = "/agent/v1/artifacts/" + spec.DeployID
		spec.ArtifactSHA256 = *r.ArtifactSHA256
	}
	return spec, nil
}

// syncNode sends the full desired state of a node: every bot it should run.
func (h *Hub) syncNode(ctx context.Context, c *agentConn) {
	rows, err := h.s.q.ListNodeBotSpecs(ctx, c.nodeID)
	if err != nil {
		slog.Error("sync: list bots", "node", c.nodeID, "err", err)
		return
	}
	specs := make([]proto.BotSpec, 0, len(rows))
	for _, r := range rows {
		spec, err := h.buildSpec(ctx, specRow{r.Bot, r.PidsMax, r.Hardened, r.SubscriptionStatus, r.OwnerSuspended, r.ArtifactSha256})
		if err != nil {
			slog.Error("sync: build spec", "bot", r.Bot.ID, "err", err)
			continue
		}
		specs = append(specs, spec)
	}
	if _, err := c.request(ctx, proto.TypeSync, proto.Sync{Bots: specs}); err != nil {
		slog.Error("sync", "node", c.nodeID, "err", err)
		return
	}
	// Resume log streams for bots someone is watching.
	h.mu.Lock()
	var watched []uuid.UUID
	for id, w := range h.watchers {
		if w.nodeID == c.nodeID {
			watched = append(watched, id)
		}
	}
	h.mu.Unlock()
	for _, id := range watched {
		h.subscribeLogs(ctx, c.nodeID, id)
	}
}

// pushBot sends one bot's current spec to its node. If the node is offline the next sync carries it.
func (h *Hub) pushBot(ctx context.Context, botID uuid.UUID) {
	r, err := h.s.q.GetBotSpec(ctx, botID)
	if err != nil {
		slog.Error("push bot", "bot", botID, "err", err)
		return
	}
	if r.Bot.DeletedAt != nil {
		h.removeBot(ctx, r.Bot.NodeID, botID)
		return
	}
	c := h.conn(r.Bot.NodeID)
	if c == nil {
		return
	}
	spec, err := h.buildSpec(ctx, specRow{r.Bot, r.PidsMax, r.Hardened, r.SubscriptionStatus, r.OwnerSuspended, r.ArtifactSha256})
	if err != nil {
		slog.Error("push bot: build spec", "bot", botID, "err", err)
		return
	}
	go func() {
		if _, err := c.request(context.WithoutCancel(ctx), proto.TypeBotApply, spec); err != nil {
			slog.Error("bot.apply", "bot", botID, "err", err)
		}
	}()
}

func (h *Hub) pushBots(ctx context.Context, ids []uuid.UUID, err error) {
	if err != nil {
		slog.Error("push bots", "err", err)
		return
	}
	for _, id := range ids {
		h.pushBot(ctx, id)
	}
}

func (h *Hub) pushUser(ctx context.Context, userID uuid.UUID) {
	rows, err := h.s.q.ListUserBotIDs(ctx, userID)
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	h.pushBots(ctx, ids, err)
}

func (h *Hub) pushSubscription(ctx context.Context, subID uuid.UUID) {
	ids, err := h.s.q.ListSubscriptionBotIDs(ctx, subID)
	h.pushBots(ctx, ids, err)
}

func (h *Hub) pushPlan(ctx context.Context, planID uuid.UUID) {
	ids, err := h.s.q.ListPlanBotIDs(ctx, planID)
	h.pushBots(ctx, ids, err)
}

func (h *Hub) removeBot(ctx context.Context, nodeID, botID uuid.UUID) {
	h.mu.Lock()
	delete(h.botStats, botID)
	delete(h.minute, botID)
	h.mu.Unlock()
	c := h.conn(nodeID)
	if c == nil {
		return // the next sync omits it, which removes it
	}
	go func() {
		if _, err := c.request(context.WithoutCancel(ctx), proto.TypeBotRemove, proto.BotRef{BotID: botID.String()}); err != nil {
			slog.Error("bot.remove", "bot", botID, "err", err)
		}
	}()
}

func (h *Hub) restartBot(ctx context.Context, nodeID, botID uuid.UUID) error {
	c := h.conn(nodeID)
	if c == nil {
		return errNodeOffline
	}
	_, err := c.request(ctx, proto.TypeBotRestart, proto.BotRef{BotID: botID.String()})
	return err
}

var errNodeOffline = &apiError{http.StatusServiceUnavailable, "node_offline", "The server this bot runs on is offline. Try again when it is back."}

// ---------- Browsers watching a bot ----------

// watch subscribes to a bot's live events. The returned buffer holds recent log lines; cancel
// must be called when the browser goes away.
func (h *Hub) watch(ctx context.Context, nodeID, botID uuid.UUID) (<-chan streamEvent, []proto.LogLine, func()) {
	ch := make(chan streamEvent, 256)
	h.mu.Lock()
	w := h.watchers[botID]
	first := w == nil
	if first {
		w = &botWatch{nodeID: nodeID, subs: map[chan streamEvent]struct{}{}}
		h.watchers[botID] = w
	}
	w.subs[ch] = struct{}{}
	buffered := append([]proto.LogLine(nil), w.logs...)
	h.mu.Unlock()

	if first {
		go h.subscribeLogs(context.WithoutCancel(ctx), nodeID, botID)
	}
	return ch, buffered, func() {
		h.mu.Lock()
		delete(w.subs, ch)
		last := len(w.subs) == 0 && h.watchers[botID] == w
		if last {
			delete(h.watchers, botID)
		}
		h.mu.Unlock()
		if last {
			if c := h.conn(nodeID); c != nil {
				go func() {
					_, _ = c.request(context.Background(), proto.TypeLogsUnsubscribe, proto.BotRef{BotID: botID.String()})
				}()
			}
		}
	}
}

// subscribeLogs asks the agent for a bot's recent output and to stream new lines.
func (h *Hub) subscribeLogs(ctx context.Context, nodeID, botID uuid.UUID) {
	c := h.conn(nodeID)
	if c == nil {
		return
	}
	data, err := c.request(ctx, proto.TypeLogsSubscribe, proto.LogsSubscribe{BotID: botID.String(), Tail: 300})
	if err != nil {
		slog.Warn("logs.subscribe", "bot", botID, "err", err)
		return
	}
	var tail proto.LogBatch
	_ = json.Unmarshal(data, &tail)
	h.mu.Lock()
	if w := h.watchers[botID]; w != nil {
		w.logs = tail.Lines
	}
	h.mu.Unlock()
	h.publish(botID, streamEvent{Type: "logs.reset", Data: tail.Lines})
}

func (h *Hub) publish(botID uuid.UUID, ev streamEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	w := h.watchers[botID]
	if w == nil {
		return
	}
	for ch := range w.subs {
		select {
		case ch <- ev:
		default: // a slow browser misses events rather than stalling the agent
		}
	}
}

func (h *Hub) liveBotStats(botID uuid.UUID) (proto.BotStats, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st, ok := h.botStats[botID]
	return st, ok
}
