package panel

import (
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/jub0t/mechon/internal/auth"
	"github.com/jub0t/mechon/internal/db"
)

const apiKeyPrefix = "mk_"

type apiKeyJSON struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

func toKeyJSON(k db.ApiKey) apiKeyJSON {
	return apiKeyJSON{ID: k.ID, Name: k.Name, Prefix: k.Prefix, Scopes: k.Scopes, LastUsedAt: k.LastUsedAt, ExpiresAt: k.ExpiresAt, CreatedAt: k.CreatedAt}
}

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.q.ListAPIKeys(r.Context(), currentPrincipal(r).user.ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]apiKeyJSON, 0, len(keys))
	for _, k := range keys {
		out = append(out, toKeyJSON(k))
	}
	writeJSON(w, http.StatusOK, out)
}

// createKey returns the secret once; only its hash is stored.
func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name          string   `json:"name"`
		Scopes        []string `json:"scopes"`
		ExpiresInDays int      `json:"expiresInDays"`
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	p := currentPrincipal(r)
	name, err := normName(in.Name, 60)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if len(in.Scopes) == 0 {
		writeError(w, r, errBadRequest("Pick at least one scope."))
		return
	}
	for _, sc := range in.Scopes {
		if !slices.Contains(allScopes, sc) {
			writeError(w, r, errBadRequest("Unknown scope "+sc+"."))
			return
		}
		if sc == ScopeOperator && p.user.Role != db.UserRoleAdmin {
			writeError(w, r, errForbidden)
			return
		}
	}
	slices.Sort(in.Scopes)
	in.Scopes = slices.Compact(in.Scopes)
	var expires *time.Time
	if in.ExpiresInDays > 0 {
		t := s.now().Add(time.Duration(in.ExpiresInDays) * 24 * time.Hour)
		expires = &t
	}
	secret := apiKeyPrefix + auth.NewToken()
	k, err := s.q.CreateAPIKey(r.Context(), db.CreateAPIKeyParams{
		UserID: p.user.ID, Name: name, Prefix: secret[:10], Hash: auth.HashToken(secret), Scopes: in.Scopes, ExpiresAt: expires,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		apiKeyJSON
		Secret string `json:"secret"`
	}{toKeyJSON(k), secret})
}

func (s *Server) deleteKey(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	n, err := s.q.DeleteAPIKey(r.Context(), db.DeleteAPIKeyParams{ID: id, UserID: currentPrincipal(r).user.ID})
	if err != nil {
		writeError(w, r, err)
		return
	}
	if n == 0 {
		writeError(w, r, errNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
