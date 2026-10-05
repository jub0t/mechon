package panel

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jub0t/mechon/internal/auth"
	"github.com/jub0t/mechon/internal/db"
)

const (
	sessionCookie = "mechon_session"
	sessionTTL    = 30 * 24 * time.Hour
	touchEvery    = time.Hour // extend the sliding expiry at most this often, not on every request
)

// API key scopes. Cookie sessions have every scope their role allows.
const (
	ScopeRead     = "bots:read"  // read bots, deploys, logs, metrics
	ScopeWrite    = "bots:write" // create, change and deploy bots
	ScopeOperator = "operator"   // admin endpoints: users, plans, nodes (admins only)
)

var allScopes = []string{ScopeRead, ScopeWrite, ScopeOperator}

type ctxKey struct{}

// principal is who is calling: a signed-in browser (tokenHash set) or an API key (keyScopes set).
type principal struct {
	user      db.User
	tokenHash []byte   // session cookie
	keyScopes []string // API key; nil for sessions
}

func (p *principal) isAPIKey() bool { return p.keyScopes != nil }

func (p *principal) can(scope string) bool {
	if scope == ScopeOperator && p.user.Role != db.UserRoleAdmin {
		return false
	}
	return !p.isAPIKey() || slices.Contains(p.keyScopes, scope)
}

func (p *principal) isAdmin() bool { return p.user.Role == db.UserRoleAdmin && p.can(ScopeOperator) }

func currentPrincipal(r *http.Request) *principal {
	p, _ := r.Context().Value(ctxKey{}).(*principal)
	return p
}

// loadPrincipal attaches the caller, if any, to the request context: an API key from
// "Authorization: Bearer mk_…" or else the session cookie.
func (s *Server) loadPrincipal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			p, err := s.principalFromKey(r.Context(), strings.TrimSpace(bearer))
			if err != nil {
				writeError(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
			return
		}

		c, err := r.Cookie(sessionCookie)
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		hash := auth.HashToken(c.Value)
		row, err := s.q.GetSessionUser(r.Context(), hash)
		if errors.Is(err, pgx.ErrNoRows) {
			s.clearCookie(w)
			next.ServeHTTP(w, r)
			return
		}
		if err != nil {
			writeError(w, r, err)
			return
		}
		if row.User.SuspendedAt != nil {
			_ = s.q.DeleteSession(r.Context(), hash)
			s.clearCookie(w)
			next.ServeHTTP(w, r)
			return
		}
		if now := s.now(); now.Sub(row.LastSeenAt) > touchEvery {
			exp := now.Add(sessionTTL)
			if err := s.q.TouchSession(r.Context(), db.TouchSessionParams{TokenHash: hash, ExpiresAt: exp}); err != nil {
				slog.Warn("touch session", "err", err)
			} else {
				s.setCookie(w, c.Value, exp)
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, &principal{user: row.User, tokenHash: hash})))
	})
}

var errBadKey = &apiError{http.StatusUnauthorized, "bad_api_key", "That API key is not valid."}

func (s *Server) principalFromKey(ctx context.Context, key string) (*principal, error) {
	if !strings.HasPrefix(key, apiKeyPrefix) {
		return nil, errBadKey
	}
	row, err := s.q.GetAPIKeyUser(ctx, auth.HashToken(key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errBadKey
	}
	if err != nil {
		return nil, err
	}
	if row.User.SuspendedAt != nil {
		return nil, &apiError{http.StatusForbidden, "suspended", "This account is suspended."}
	}
	if row.LastUsedAt == nil || s.now().Sub(*row.LastUsedAt) > time.Minute {
		if err := s.q.TouchAPIKey(ctx, row.KeyID); err != nil {
			slog.Warn("touch api key", "err", err)
		}
	}
	return &principal{user: row.User, keyScopes: row.Scopes}, nil
}

// handler wraps an endpoint with the access it needs. scope "" means any signed-in caller;
// sessionOnly endpoints (password, API keys) refuse API keys.
type access struct {
	scope       string
	sessionOnly bool
}

func (s *Server) guard(a access, h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := currentPrincipal(r)
		switch {
		case p == nil:
			writeError(w, r, errUnauthorized)
		case a.sessionOnly && p.isAPIKey():
			writeError(w, r, &apiError{http.StatusForbidden, "session_required", "Sign in to the panel to do this; API keys cannot."})
		case a.scope != "" && !p.can(a.scope):
			writeError(w, r, errForbidden)
		default:
			h(w, r)
		}
	})
}

func (s *Server) setCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.cfg.SecureCookies(), SameSite: http.SameSiteLaxMode,
	})
}
