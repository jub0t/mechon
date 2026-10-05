package panel

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jub0t/mechon/internal/auth"
	"github.com/jub0t/mechon/internal/db"
	"github.com/jub0t/mechon/internal/templates"
)

// ---------- Plans ----------

type planJSON struct {
	ID                uuid.UUID `json:"id"`
	Slug              string    `json:"slug"`
	Name              string    `json:"name"`
	MaxBots           int32     `json:"maxBots"`
	MemoryMB          int32     `json:"memoryMb"`
	CPUMillicores     int32     `json:"cpuMillicores"`
	DiskMB            int32     `json:"diskMb"`
	PidsMax           int32     `json:"pidsMax"`
	Hardened          bool      `json:"hardened"`
	Templates         []string  `json:"templates"`
	SubscriptionCount int32     `json:"subscriptionCount"`
	CreatedAt         time.Time `json:"createdAt"`
}

func toPlanJSON(p db.Plan, subs int32) planJSON {
	return planJSON{ID: p.ID, Slug: p.Slug, Name: p.Name, MaxBots: p.MaxBots, MemoryMB: p.MemoryMb, CPUMillicores: p.CpuMillicores,
		DiskMB: p.DiskMb, PidsMax: p.PidsMax, Hardened: p.Hardened, Templates: p.Templates, SubscriptionCount: subs, CreatedAt: p.CreatedAt}
}

type planInput struct {
	Slug          string   `json:"slug"`
	Name          string   `json:"name"`
	MaxBots       int32    `json:"maxBots"`
	MemoryMB      int32    `json:"memoryMb"`
	CPUMillicores int32    `json:"cpuMillicores"`
	DiskMB        int32    `json:"diskMb"`
	PidsMax       int32    `json:"pidsMax"`
	Hardened      bool     `json:"hardened"`
	Templates     []string `json:"templates"`
}

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func (in *planInput) validate(creating bool) error {
	var err error
	if in.Name, err = normName(in.Name, 60); err != nil {
		return err
	}
	if creating && !slugRe.MatchString(in.Slug) {
		return errBadRequest("The slug must be lowercase letters, digits and dashes, e.g. starter-512.")
	}
	switch {
	case in.MaxBots < 1 || in.MaxBots > 1000:
		return errBadRequest("Bots must be between 1 and 1000.")
	case in.MemoryMB < 64:
		return errBadRequest("Memory must be at least 64 MB.")
	case in.CPUMillicores < 50:
		return errBadRequest("CPU must be at least 0.05 cores.")
	case in.DiskMB < 128:
		return errBadRequest("Disk must be at least 128 MB.")
	}
	if in.PidsMax == 0 {
		in.PidsMax = 128
	}
	if in.PidsMax < 16 || in.PidsMax > 4096 {
		return errBadRequest("Process limit must be between 16 and 4096.")
	}
	if in.Templates == nil {
		in.Templates = []string{}
	}
	for _, t := range in.Templates {
		if _, ok := templates.Get(t); !ok {
			return errBadRequest("Unknown template " + t + ".")
		}
	}
	return nil
}

func (s *Server) listPlans(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListPlans(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]planJSON, 0, len(rows))
	for _, p := range rows {
		out = append(out, toPlanJSON(db.Plan{ID: p.ID, Slug: p.Slug, Name: p.Name, MaxBots: p.MaxBots, MemoryMb: p.MemoryMb,
			CpuMillicores: p.CpuMillicores, DiskMb: p.DiskMb, PidsMax: p.PidsMax, Hardened: p.Hardened, Templates: p.Templates,
			CreatedAt: p.CreatedAt}, p.SubscriptionCount))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createPlan(w http.ResponseWriter, r *http.Request) {
	var in planInput
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	if err := in.validate(true); err != nil {
		writeError(w, r, err)
		return
	}
	p, err := s.q.CreatePlan(r.Context(), db.CreatePlanParams{Slug: in.Slug, Name: in.Name, MaxBots: in.MaxBots, MemoryMb: in.MemoryMB,
		CpuMillicores: in.CPUMillicores, DiskMb: in.DiskMB, PidsMax: in.PidsMax, Hardened: in.Hardened, Templates: in.Templates})
	if uniqueViolation(err) {
		writeError(w, r, errConflict("A plan with that slug already exists."))
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toPlanJSON(p, 0))
}

// updatePlan changes limits for every subscriber. Shrinking below what bots already use is allowed;
// it blocks new allocations until usage fits again, and never stops running bots.
func (s *Server) updatePlan(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in planInput
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	if err := in.validate(false); err != nil {
		writeError(w, r, err)
		return
	}
	p, err := s.q.UpdatePlan(r.Context(), db.UpdatePlanParams{ID: id, Name: in.Name, MaxBots: in.MaxBots, MemoryMb: in.MemoryMB,
		CpuMillicores: in.CPUMillicores, DiskMb: in.DiskMB, PidsMax: in.PidsMax, Hardened: in.Hardened, Templates: in.Templates})
	if err != nil {
		writeError(w, r, notFoundIfNoRows(err))
		return
	}
	writeJSON(w, http.StatusOK, toPlanJSON(p, 0))
	// pids and hardening are part of each bot's spec.
	s.hub.pushPlan(context.WithoutCancel(r.Context()), id)
}

func (s *Server) archivePlan(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.q.ArchivePlan(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- Users ----------

type adminUserJSON struct {
	userJSON
	SubscriptionCount int32 `json:"subscriptionCount"`
	BotCount          int32 `json:"botCount"`
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListUsers(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]adminUserJSON, 0, len(rows))
	for _, u := range rows {
		out = append(out, adminUserJSON{
			userJSON: toUserJSON(db.User{ID: u.ID, Email: u.Email, Name: u.Name, PasswordHash: u.PasswordHash, Role: u.Role,
				ExternalID: u.ExternalID, SuspendedAt: u.SuspendedAt, CreatedAt: u.CreatedAt}),
			SubscriptionCount: u.SubscriptionCount, BotCount: u.BotCount,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// createUser makes an account. A password is optional: billing systems create accounts first and
// the customer sets a password later (an admin can reset one any time). An optional planId
// subscribes the new user straight away.
func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email      string      `json:"email"`
		Name       string      `json:"name"`
		Password   string      `json:"password"`
		Role       db.UserRole `json:"role"`
		ExternalID string      `json:"externalId"`
		PlanID     *uuid.UUID  `json:"planId"`
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	email, err := normEmail(in.Email)
	if err != nil {
		writeError(w, r, err)
		return
	}
	name, err := normName(in.Name, 80)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if in.Role == "" {
		in.Role = db.UserRoleUser
	}
	if in.Role != db.UserRoleUser && in.Role != db.UserRoleAdmin {
		writeError(w, r, errBadRequest("Role must be admin or user."))
		return
	}
	var hash *string
	if in.Password != "" {
		if err := checkNewPassword(in.Password); err != nil {
			writeError(w, r, err)
			return
		}
		h, err := auth.HashPassword(in.Password)
		if err != nil {
			writeError(w, r, err)
			return
		}
		hash = &h
	}
	var ext *string
	if e := strings.TrimSpace(in.ExternalID); e != "" {
		ext = &e
	}

	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer tx.Rollback(context.WithoutCancel(r.Context()))
	q := s.q.WithTx(tx)
	u, err := q.CreateUserFull(r.Context(), db.CreateUserFullParams{Email: email, Name: name, PasswordHash: hash, Role: in.Role, ExternalID: ext})
	if uniqueViolation(err) {
		writeError(w, r, errConflict("An account with that email or external id already exists."))
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	if in.PlanID != nil {
		if _, err := s.subscribe(r.Context(), q, u.ID, *in.PlanID, nil); err != nil {
			writeError(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toUserJSON(u))
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	u, err := s.q.GetUserByID(r.Context(), id)
	if err != nil {
		writeError(w, r, notFoundIfNoRows(err))
		return
	}
	writeJSON(w, http.StatusOK, toUserJSON(u))
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in struct {
		Name  string      `json:"name"`
		Email string      `json:"email"`
		Role  db.UserRole `json:"role"`
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	name, err := normName(in.Name, 80)
	if err != nil {
		writeError(w, r, err)
		return
	}
	email, err := normEmail(in.Email)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if in.Role != db.UserRoleUser && in.Role != db.UserRoleAdmin {
		writeError(w, r, errBadRequest("Role must be admin or user."))
		return
	}
	if id == currentPrincipal(r).user.ID && in.Role != db.UserRoleAdmin {
		writeError(w, r, errBadRequest("You cannot remove your own admin role."))
		return
	}
	u, err := s.q.UpdateUserProfile(r.Context(), db.UpdateUserProfileParams{ID: id, Name: name, Email: email})
	if uniqueViolation(err) {
		writeError(w, r, errConflict("Another account already uses that email."))
		return
	}
	if err != nil {
		writeError(w, r, notFoundIfNoRows(err))
		return
	}
	if u.Role != in.Role {
		if u, err = s.q.UpdateUserRole(r.Context(), db.UpdateUserRoleParams{ID: id, Role: in.Role}); err != nil {
			writeError(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, toUserJSON(u))
}

// deleteUser removes an account that has no bots left. Delete or move the bots first.
func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	if id == currentPrincipal(r).user.ID {
		writeError(w, r, errBadRequest("You cannot delete your own account."))
		return
	}
	bots, err := s.q.ListUserBotIDs(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if len(bots) > 0 {
		writeError(w, r, errConflict("This user still has bots. Delete them first."))
		return
	}
	if err := s.q.DeleteUser(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	if err := checkNewPassword(in.Password); err != nil {
		writeError(w, r, err)
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if _, err := s.q.GetUserByID(r.Context(), id); err != nil {
		writeError(w, r, notFoundIfNoRows(err))
		return
	}
	if err := s.q.SetUserPassword(r.Context(), db.SetUserPasswordParams{ID: id, PasswordHash: &hash}); err != nil {
		writeError(w, r, err)
		return
	}
	// Everyone signed in as that user is signed out.
	if err := s.q.DeleteUserSessions(r.Context(), db.DeleteUserSessionsParams{UserID: id, TokenHash: []byte{}}); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// suspendUser blocks sign-in and stops every bot the user has; unsuspending lets them start again.
func (s *Server) suspendUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in struct {
		Suspended bool `json:"suspended"`
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	if id == currentPrincipal(r).user.ID {
		writeError(w, r, errBadRequest("You cannot suspend yourself."))
		return
	}
	u, err := s.q.SetUserSuspended(r.Context(), db.SetUserSuspendedParams{ID: id, Suspended: in.Suspended})
	if err != nil {
		writeError(w, r, notFoundIfNoRows(err))
		return
	}
	writeJSON(w, http.StatusOK, toUserJSON(u))
	s.hub.pushUser(context.WithoutCancel(r.Context()), id)
}

// ---------- Subscriptions ----------

type subscriptionJSON struct {
	ID         uuid.UUID             `json:"id"`
	Status     db.SubscriptionStatus `json:"status"`
	ExternalID *string               `json:"externalId"`
	Plan       planJSON              `json:"plan"`
	Limits     limitsJSON            `json:"limits"`    // the plan with this customer's overrides applied
	Overrides  overridesJSON         `json:"overrides"` // null fields use the plan
	Note       string                `json:"note"`
	Used       usageJSON             `json:"used"`
	CreatedAt  time.Time             `json:"createdAt"`
}

type limitsJSON struct {
	MaxBots       int32 `json:"maxBots"`
	MemoryMB      int32 `json:"memoryMb"`
	CPUMillicores int32 `json:"cpuMillicores"`
	DiskMB        int32 `json:"diskMb"`
	PidsMax       int32 `json:"pidsMax"`
}

type overridesJSON struct {
	MaxBots       *int32 `json:"maxBots"`
	MemoryMB      *int32 `json:"memoryMb"`
	CPUMillicores *int32 `json:"cpuMillicores"`
	DiskMB        *int32 `json:"diskMb"`
	PidsMax       *int32 `json:"pidsMax"`
}

func (o overridesJSON) validate() error {
	check := func(v *int32, min, max int32, what string) error {
		if v != nil && (*v < min || *v > max) {
			return errBadRequest(what)
		}
		return nil
	}
	return errors.Join(
		check(o.MaxBots, 1, 1000, "Bots must be between 1 and 1000."),
		check(o.MemoryMB, 64, 1<<20, "Memory must be at least 64 MB."),
		check(o.CPUMillicores, 50, 1<<20, "CPU must be at least 0.05 cores."),
		check(o.DiskMB, 128, 1<<24, "Disk must be at least 128 MB."),
		check(o.PidsMax, 16, 4096, "Process limit must be between 16 and 4096."),
	)
}

// effectivePlan applies a subscription's per-customer overrides to its plan.
func effectivePlan(sub db.Subscription, p db.Plan) db.Plan {
	pick := func(o *int32, v int32) int32 {
		if o != nil {
			return *o
		}
		return v
	}
	p.MaxBots = pick(sub.MaxBots, p.MaxBots)
	p.MemoryMb = pick(sub.MemoryMb, p.MemoryMb)
	p.CpuMillicores = pick(sub.CpuMillicores, p.CpuMillicores)
	p.DiskMb = pick(sub.DiskMb, p.DiskMb)
	p.PidsMax = pick(sub.PidsMax, p.PidsMax)
	return p
}

type usageJSON struct {
	Bots          int32 `json:"bots"`
	MemoryMB      int32 `json:"memoryMb"`
	CPUMillicores int32 `json:"cpuMillicores"`
	DiskMB        int32 `json:"diskMb"`
}

func (s *Server) subscriptionsFor(ctx context.Context, userID uuid.UUID) ([]subscriptionJSON, error) {
	rows, err := s.q.ListSubscriptionsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]subscriptionJSON, 0, len(rows))
	for _, r := range rows {
		eff := effectivePlan(r.Subscription, r.Plan)
		sub := r.Subscription
		out = append(out, subscriptionJSON{
			ID: sub.ID, Status: sub.Status, ExternalID: sub.ExternalID,
			Plan:      toPlanJSON(r.Plan, 0),
			Limits:    limitsJSON{eff.MaxBots, eff.MemoryMb, eff.CpuMillicores, eff.DiskMb, eff.PidsMax},
			Overrides: overridesJSON{sub.MaxBots, sub.MemoryMb, sub.CpuMillicores, sub.DiskMb, sub.PidsMax},
			Note:      sub.Note,
			Used:      usageJSON{Bots: r.UsedBots, MemoryMB: r.UsedMemoryMb, CPUMillicores: r.UsedCpuMillicores, DiskMB: r.UsedDiskMb},
			CreatedAt: r.Subscription.CreatedAt,
		})
	}
	return out, nil
}

func (s *Server) mySubscriptions(w http.ResponseWriter, r *http.Request) {
	out, err := s.subscriptionsFor(r.Context(), currentPrincipal(r).user.ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) userSubscriptions(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	out, err := s.subscriptionsFor(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) subscribe(ctx context.Context, q *db.Queries, userID, planID uuid.UUID, externalID *string) (db.Subscription, error) {
	plan, err := q.GetPlan(ctx, planID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && plan.ArchivedAt != nil) {
		return db.Subscription{}, errBadRequest("That plan does not exist.")
	}
	if err != nil {
		return db.Subscription{}, err
	}
	sub, err := q.CreateSubscription(ctx, db.CreateSubscriptionParams{UserID: userID, PlanID: planID, ExternalID: externalID})
	if uniqueViolation(err) {
		return db.Subscription{}, errConflict("A subscription with that external id already exists.")
	}
	return sub, err
}

func (s *Server) createSubscription(w http.ResponseWriter, r *http.Request) {
	userID, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in struct {
		PlanID     uuid.UUID `json:"planId"`
		ExternalID string    `json:"externalId"`
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	if _, err := s.q.GetUserByID(r.Context(), userID); err != nil {
		writeError(w, r, notFoundIfNoRows(err))
		return
	}
	var ext *string
	if e := strings.TrimSpace(in.ExternalID); e != "" {
		ext = &e
	}
	sub, err := s.subscribe(r.Context(), s.q, userID, in.PlanID, ext)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": sub.ID, "status": sub.Status})
}

// updateSubscription suspends, unsuspends, terminates or changes the plan of a subscription.
// Suspending stops its bots; terminating stops them and they are deleted.
func (s *Server) updateSubscription(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in struct {
		Status    *db.SubscriptionStatus `json:"status"`
		PlanID    *uuid.UUID             `json:"planId"`
		Overrides *overridesJSON         `json:"overrides"` // replaces all overrides; null fields use the plan
		Note      *string                `json:"note"`
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	ctx := r.Context()
	sub, err := s.q.GetSubscription(ctx, id)
	if err != nil {
		writeError(w, r, notFoundIfNoRows(err))
		return
	}
	if sub.Status == db.SubscriptionStatusTerminated {
		writeError(w, r, errConflict("This subscription is terminated."))
		return
	}
	if in.PlanID != nil {
		plan, err := s.q.GetPlan(ctx, *in.PlanID)
		if err != nil || plan.ArchivedAt != nil {
			writeError(w, r, errBadRequest("That plan does not exist."))
			return
		}
		if sub, err = s.q.SetSubscriptionPlan(ctx, db.SetSubscriptionPlanParams{ID: id, PlanID: *in.PlanID}); err != nil {
			writeError(w, r, err)
			return
		}
	}
	if in.Overrides != nil || in.Note != nil {
		o := overridesJSON{sub.MaxBots, sub.MemoryMb, sub.CpuMillicores, sub.DiskMb, sub.PidsMax}
		if in.Overrides != nil {
			o = *in.Overrides
		}
		if err := o.validate(); err != nil {
			writeError(w, r, err)
			return
		}
		note := sub.Note
		if in.Note != nil {
			note = truncate(strings.TrimSpace(*in.Note), 500)
		}
		if sub, err = s.q.SetSubscriptionOverrides(ctx, db.SetSubscriptionOverridesParams{ID: id, MaxBots: o.MaxBots, MemoryMb: o.MemoryMB,
			CpuMillicores: o.CPUMillicores, DiskMb: o.DiskMB, PidsMax: o.PidsMax, Note: note}); err != nil {
			writeError(w, r, err)
			return
		}
	}
	if in.Status != nil {
		if !slices.Contains([]db.SubscriptionStatus{db.SubscriptionStatusActive, db.SubscriptionStatusSuspended, db.SubscriptionStatusTerminated}, *in.Status) {
			writeError(w, r, errBadRequest("Status must be active, suspended or terminated."))
			return
		}
		if sub, err = s.q.SetSubscriptionStatus(ctx, db.SetSubscriptionStatusParams{ID: id, Status: *in.Status}); err != nil {
			writeError(w, r, err)
			return
		}
		if *in.Status == db.SubscriptionStatusTerminated {
			if err := s.terminateSubscriptionBots(ctx, id); err != nil {
				writeError(w, r, err)
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": sub.ID, "status": sub.Status, "planId": sub.PlanID})
	s.hub.pushSubscription(context.WithoutCancel(ctx), id)
}

func (s *Server) terminateSubscriptionBots(ctx context.Context, subID uuid.UUID) error {
	rows, err := s.q.SetBotsDesiredForSubscription(ctx, db.SetBotsDesiredForSubscriptionParams{SubscriptionID: subID, DesiredState: db.BotDesiredStateStopped})
	if err != nil {
		return err
	}
	for _, b := range rows {
		if err := s.q.SoftDeleteBot(ctx, b.ID); err != nil {
			return err
		}
		s.hub.removeBot(context.WithoutCancel(ctx), b.NodeID, b.ID)
	}
	return nil
}

func (s *Server) listTemplates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, templates.All())
}
