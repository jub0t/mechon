package panel

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jub0t/mechon/internal/db"
	"github.com/jub0t/mechon/internal/proto"
	"github.com/jub0t/mechon/internal/templates"
)

type botJSON struct {
	ID              uuid.UUID             `json:"id"`
	Name            string                `json:"name"`
	Template        string                `json:"template"`
	State           db.BotObservedState   `json:"state"`
	Desired         db.BotDesiredState    `json:"desired"`
	Error           string                `json:"error"`
	ExitCode        *int32                `json:"exitCode"`
	Restarts        int32                 `json:"restarts"`
	Limits          capacityJSON          `json:"limits"`
	UID             int                   `json:"uid"`
	Node            refJSON               `json:"node"`
	Owner           ownerJSON             `json:"owner"`
	Plan            string                `json:"plan"`
	SubscriptionID  uuid.UUID             `json:"subscriptionId"`
	Subscription    db.SubscriptionStatus `json:"subscriptionStatus,omitempty"`
	CurrentDeployID *uuid.UUID            `json:"currentDeployId"`
	StateChangedAt  time.Time             `json:"stateChangedAt"`
	CreatedAt       time.Time             `json:"createdAt"`
	Usage           *proto.BotStats       `json:"usage,omitempty"`
}

type refJSON struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type ownerJSON struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
}

func (s *Server) botView(b db.Bot, node refJSON, owner ownerJSON, plan string, sub db.SubscriptionStatus) botJSON {
	v := botJSON{
		ID: b.ID, Name: b.Name, Template: b.Template, State: b.ObservedState, Desired: b.DesiredState,
		Error: b.ObservedError, ExitCode: b.ExitCode, Restarts: b.RestartCount,
		Limits: capacityJSON{b.MemoryMb, b.CpuMillicores, b.DiskMb}, UID: 100000 + int(b.UidSeq),
		Node: node, Owner: owner, Plan: plan, SubscriptionID: b.SubscriptionID, Subscription: sub,
		CurrentDeployID: b.CurrentDeployID, StateChangedAt: b.StateChangedAt, CreatedAt: b.CreatedAt,
	}
	if st, ok := s.hub.liveBotStats(b.ID); ok && b.ObservedState == db.BotObservedStateRunning {
		v.Usage = &st
	}
	return v
}

// loadBot fetches a bot the caller may see. Other users' bots are reported as not found.
func (s *Server) loadBot(r *http.Request) (db.GetBotRow, error) {
	id, err := pathID(r, "id")
	if err != nil {
		return db.GetBotRow{}, err
	}
	row, err := s.q.GetBot(r.Context(), id)
	if err != nil {
		return row, notFoundIfNoRows(err)
	}
	p := currentPrincipal(r)
	if row.OwnerID != p.user.ID && !p.isAdmin() {
		return row, errNotFound
	}
	return row, nil
}

func (s *Server) getBotView(row db.GetBotRow) botJSON {
	return s.botView(row.Bot, refJSON{row.Bot.NodeID, row.NodeName}, ownerJSON{row.OwnerID, row.OwnerName, row.OwnerEmail}, row.PlanName, row.SubscriptionStatus)
}

// listBots returns every bot for admins (or only their own with ?mine=1) and own bots for users.
func (s *Server) listBots(w http.ResponseWriter, r *http.Request) {
	p := currentPrincipal(r)
	var owner *uuid.UUID
	if !p.isAdmin() || r.URL.Query().Get("mine") == "1" {
		owner = &p.user.ID
	}
	rows, err := s.q.ListBots(r.Context(), owner)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]botJSON, 0, len(rows))
	for _, b := range rows {
		out = append(out, s.botView(b.Bot, refJSON{b.Bot.NodeID, b.NodeName}, ownerJSON{b.OwnerID, b.OwnerName, b.OwnerEmail}, b.PlanName, ""))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getBot(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadBot(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.getBotView(row))
}

var errNoRoom = &apiError{http.StatusConflict, "no_capacity", "No server has room for a bot this size right now. Ask your host to add capacity."}

type sizeInput struct {
	MemoryMB      int32 `json:"memoryMb"`
	CPUMillicores int32 `json:"cpuMillicores"`
	DiskMB        int32 `json:"diskMb"`
}

func (in sizeInput) validate() error {
	switch {
	case in.MemoryMB < 64:
		return errBadRequest("Give the bot at least 64 MB of memory.")
	case in.CPUMillicores < 50:
		return errBadRequest("Give the bot at least 0.05 cores.")
	case in.DiskMB < 128:
		return errBadRequest("Give the bot at least 128 MB of disk.")
	}
	return nil
}

// checkQuota verifies a bot of this size fits the subscription's plan, counting every other bot
// on it. Callers hold the subscription row lock.
func checkQuota(ctx context.Context, q *db.Queries, sub db.Subscription, plan db.Plan, size sizeInput, excludeBot uuid.UUID, adding bool) error {
	used, err := q.SubscriptionUsage(ctx, db.SubscriptionUsageParams{SubscriptionID: sub.ID, ExcludeBotID: excludeBot})
	if err != nil {
		return err
	}
	over := func(what string) error {
		return &apiError{http.StatusConflict, "quota", "That goes over your plan's " + what + ". Pick a smaller size or upgrade the plan."}
	}
	switch {
	case adding && used.Bots+1 > plan.MaxBots:
		return &apiError{http.StatusConflict, "quota", "Your plan allows " + strconv.Itoa(int(plan.MaxBots)) + " bots and you are using all of them."}
	case used.MemoryMb+size.MemoryMB > plan.MemoryMb:
		return over("memory")
	case used.CpuMillicores+size.CPUMillicores > plan.CpuMillicores:
		return over("CPU")
	case used.DiskMb+size.DiskMB > plan.DiskMb:
		return over("disk")
	}
	return nil
}

var botNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9 _.-]{0,47}$`)

func (s *Server) createBot(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SubscriptionID uuid.UUID         `json:"subscriptionId"`
		Name           string            `json:"name"`
		Template       string            `json:"template"`
		Env            map[string]string `json:"env"`
		sizeInput
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if !botNameRe.MatchString(in.Name) {
		writeError(w, r, errBadRequest("Name the bot with letters, digits, spaces, dots, dashes or underscores (up to 48)."))
		return
	}
	tpl, ok := templates.Get(in.Template)
	if !ok {
		writeError(w, r, errBadRequest("Pick a template."))
		return
	}
	if err := in.sizeInput.validate(); err != nil {
		writeError(w, r, err)
		return
	}
	vars, err := envFromMap(in.Env, tpl)
	if err != nil {
		writeError(w, r, err)
		return
	}

	ctx := r.Context()
	p := currentPrincipal(r)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	q := s.q.WithTx(tx)

	sub, err := q.GetSubscriptionForUpdate(ctx, in.SubscriptionID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && sub.UserID != p.user.ID && !p.isAdmin()) {
		writeError(w, r, errBadRequest("Pick one of your plans."))
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	if sub.Status != db.SubscriptionStatusActive {
		writeError(w, r, &apiError{http.StatusConflict, "subscription_inactive", "This plan is " + string(sub.Status) + ". Contact your host."})
		return
	}
	plan, err := q.GetPlan(ctx, sub.PlanID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	plan = effectivePlan(sub, plan)
	if len(plan.Templates) > 0 && !slices.Contains(plan.Templates, tpl.ID) {
		writeError(w, r, errBadRequest("Your plan does not include the "+tpl.Name+" template."))
		return
	}
	if err := checkQuota(ctx, q, sub, plan, in.sizeInput, uuid.Nil, true); err != nil {
		writeError(w, r, err)
		return
	}
	nodeID, err := place(ctx, q, plan.Hardened, in.sizeInput)
	if err != nil {
		writeError(w, r, err)
		return
	}
	bot, err := q.CreateBot(ctx, db.CreateBotParams{SubscriptionID: sub.ID, NodeID: nodeID, Name: in.Name, Template: tpl.ID,
		MemoryMb: in.MemoryMB, CpuMillicores: in.CPUMillicores, DiskMb: in.DiskMB})
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.writeEnv(ctx, q, bot.ID, vars); err != nil {
		writeError(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, r, err)
		return
	}
	row, err := s.q.GetBot(ctx, bot.ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.getBotView(row))
	s.hub.pushBot(context.WithoutCancel(ctx), bot.ID)
}

// place picks the online node with the most free memory that fits the bot, and locks it.
func place(ctx context.Context, q *db.Queries, hardened bool, size sizeInput) (uuid.UUID, error) {
	nodes, err := q.PlacementCandidates(ctx, hardened)
	if err != nil {
		return uuid.Nil, err
	}
	best, bestFree := uuid.Nil, int32(-1)
	for _, n := range nodes {
		memFree := int32(float64(n.MemoryMb)*n.Overcommit) - n.UsedMemoryMb
		cpuFree := int32(float64(n.CpuMillicores)*n.Overcommit) - n.UsedCpuMillicores
		diskFree := n.DiskMb - n.UsedDiskMb // disk is real space: never overcommitted
		if memFree < size.MemoryMB || cpuFree < size.CPUMillicores || diskFree < size.DiskMB {
			continue
		}
		if memFree > bestFree {
			best, bestFree = n.ID, memFree
		}
	}
	if best == uuid.Nil {
		return uuid.Nil, errNoRoom
	}
	return best, nil
}

// updateBot renames or resizes a bot. Sizes are checked against the plan and the bot's node.
// Disk can grow but not shrink.
func (s *Server) updateBot(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadBot(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in struct {
		Name string `json:"name"`
		sizeInput
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if !botNameRe.MatchString(in.Name) {
		writeError(w, r, errBadRequest("Name the bot with letters, digits, spaces, dots, dashes or underscores (up to 48)."))
		return
	}
	if err := in.sizeInput.validate(); err != nil {
		writeError(w, r, err)
		return
	}
	if in.DiskMB < row.Bot.DiskMb {
		writeError(w, r, errBadRequest("Disk can grow but not shrink."))
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	q := s.q.WithTx(tx)
	sub, err := q.GetSubscriptionForUpdate(ctx, row.Bot.SubscriptionID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	plan, err := q.GetPlan(ctx, sub.PlanID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := checkQuota(ctx, q, sub, effectivePlan(sub, plan), in.sizeInput, row.Bot.ID, false); err != nil {
		writeError(w, r, err)
		return
	}
	grow := in.MemoryMB > row.Bot.MemoryMb || in.CPUMillicores > row.Bot.CpuMillicores || in.DiskMB > row.Bot.DiskMb
	if grow {
		if err := fitsNode(ctx, q, row.Bot, in.sizeInput); err != nil {
			writeError(w, r, err)
			return
		}
	}
	if _, err := q.UpdateBotSettings(ctx, db.UpdateBotSettingsParams{ID: row.Bot.ID, Name: in.Name, MemoryMb: in.MemoryMB,
		CpuMillicores: in.CPUMillicores, DiskMb: in.DiskMB}); err != nil {
		writeError(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, r, err)
		return
	}
	row, _ = s.q.GetBot(ctx, row.Bot.ID)
	writeJSON(w, http.StatusOK, s.getBotView(row))
	s.hub.pushBot(context.WithoutCancel(ctx), row.Bot.ID)
}

func fitsNode(ctx context.Context, q *db.Queries, bot db.Bot, size sizeInput) error {
	nodes, err := q.PlacementCandidates(ctx, false)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if n.ID != bot.NodeID {
			continue
		}
		memFree := int32(float64(n.MemoryMb)*n.Overcommit) - n.UsedMemoryMb + bot.MemoryMb
		cpuFree := int32(float64(n.CpuMillicores)*n.Overcommit) - n.UsedCpuMillicores + bot.CpuMillicores
		diskFree := n.DiskMb - n.UsedDiskMb + bot.DiskMb
		if memFree >= size.MemoryMB && cpuFree >= size.CPUMillicores && diskFree >= size.DiskMB {
			return nil
		}
		return &apiError{http.StatusConflict, "no_capacity", "The server this bot runs on does not have room for that size."}
	}
	return errNodeOffline
}

func (s *Server) deleteBot(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadBot(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.q.SoftDeleteBot(r.Context(), row.Bot.ID); err != nil {
		writeError(w, r, err)
		return
	}
	s.hub.removeBot(context.WithoutCancel(r.Context()), row.Bot.NodeID, row.Bot.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) botAction(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadBot(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in struct {
		Action string `json:"action"`
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	ctx := r.Context()
	if in.Action != "stop" && row.SubscriptionStatus != db.SubscriptionStatusActive {
		writeError(w, r, &apiError{http.StatusConflict, "subscription_inactive", "This bot's plan is " + string(row.SubscriptionStatus) + ". Contact your host."})
		return
	}
	switch in.Action {
	case "start", "stop":
		desired := db.BotDesiredStateRunning
		if in.Action == "stop" {
			desired = db.BotDesiredStateStopped
		}
		if err := s.q.SetBotDesired(ctx, db.SetBotDesiredParams{ID: row.Bot.ID, DesiredState: desired}); err != nil {
			writeError(w, r, err)
			return
		}
		s.hub.pushBot(context.WithoutCancel(ctx), row.Bot.ID)
	case "restart":
		if row.Bot.DesiredState != db.BotDesiredStateRunning {
			if err := s.q.SetBotDesired(ctx, db.SetBotDesiredParams{ID: row.Bot.ID, DesiredState: db.BotDesiredStateRunning}); err != nil {
				writeError(w, r, err)
				return
			}
			s.hub.pushBot(context.WithoutCancel(ctx), row.Bot.ID)
		} else if err := s.hub.restartBot(ctx, row.Bot.NodeID, row.Bot.ID); err != nil {
			writeError(w, r, err)
			return
		}
	default:
		writeError(w, r, errBadRequest("Action must be start, stop or restart."))
		return
	}
	row, _ = s.q.GetBot(ctx, row.Bot.ID)
	writeJSON(w, http.StatusOK, s.getBotView(row))
}

func (s *Server) botMetrics(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadBot(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	span := map[string]time.Duration{"1h": time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour}[r.URL.Query().Get("range")]
	if span == 0 {
		span = time.Hour
	}
	rows, err := s.q.ListBotMetrics(r.Context(), db.ListBotMetricsParams{BotID: row.Bot.ID, Ts: s.now().Add(-span)})
	if err != nil {
		writeError(w, r, err)
		return
	}
	type point struct {
		T      int64   `json:"t"`
		CPU    float32 `json:"cpu"`
		Memory int64   `json:"memory"`
		Disk   int64   `json:"disk"`
	}
	out := make([]point, 0, len(rows))
	for _, m := range rows {
		out = append(out, point{m.Ts.UnixMilli(), m.CpuPct, m.MemoryBytes, m.DiskBytes})
	}
	writeJSON(w, http.StatusOK, out)
}
