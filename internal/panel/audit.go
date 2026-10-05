package panel

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jub0t/mechon/internal/db"
)

// audit records who changed what. It never fails the request it describes.
func (s *Server) audit(r *http.Request, action, targetType, targetID, targetName string, meta map[string]any) {
	p := currentPrincipal(r)
	if p == nil {
		return
	}
	if meta == nil {
		meta = map[string]any{}
	}
	raw, _ := json.Marshal(meta)
	id := p.user.ID
	err := s.q.InsertAudit(context.WithoutCancel(r.Context()), db.InsertAuditParams{
		ActorID: &id, ActorName: p.user.Name, ViaApiKey: p.isAPIKey(), Action: action,
		TargetType: targetType, TargetID: targetID, TargetName: targetName, Metadata: raw, Ip: s.clientIP(r),
	})
	if err != nil {
		slog.Error("audit", "action", action, "err", err)
	}
}

type auditJSON struct {
	ID         int64           `json:"id"`
	ActorID    *uuid.UUID      `json:"actorId"`
	ActorName  string          `json:"actorName"`
	ViaAPIKey  bool            `json:"viaApiKey"`
	Action     string          `json:"action"`
	TargetType string          `json:"targetType"`
	TargetID   string          `json:"targetId"`
	TargetName string          `json:"targetName"`
	Metadata   json.RawMessage `json:"metadata"`
	IP         string          `json:"ip"`
	CreatedAt  time.Time       `json:"createdAt"`
}

// listAudit pages backwards with ?before=<id>, optionally filtered by ?prefix=bot.
func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	var before *int64
	if b, err := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64); err == nil {
		before = &b
	}
	prefix := strings.TrimSpace(r.URL.Query().Get("prefix"))
	rows, err := s.q.ListAudit(r.Context(), db.ListAuditParams{Before: before, Prefix: prefix})
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]auditJSON, 0, len(rows))
	for _, a := range rows {
		out = append(out, auditJSON{a.ID, a.ActorID, a.ActorName, a.ViaApiKey, a.Action, a.TargetType, a.TargetID, a.TargetName, a.Metadata, a.Ip, a.CreatedAt})
	}
	writeJSON(w, http.StatusOK, out)
}
