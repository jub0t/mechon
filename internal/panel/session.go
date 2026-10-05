package panel

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
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

type ctxKey struct{}

type session struct {
	user      db.User
	tokenHash []byte
}

func currentSession(r *http.Request) *session {
	s, _ := r.Context().Value(ctxKey{}).(*session)
	return s
}

// loadSession attaches the signed-in user, if any, to the request context.
func (s *Server) loadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		ctx := context.WithValue(r.Context(), ctxKey{}, &session{user: row.User, tokenHash: hash})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requireUser(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if currentSession(r) == nil {
			writeError(w, r, errUnauthorized)
			return
		}
		h(w, r)
	})
}

func (s *Server) requireAdmin(h http.HandlerFunc) http.Handler {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request) {
		if currentSession(r).user.Role != db.UserRoleAdmin {
			writeError(w, r, errForbidden)
			return
		}
		h(w, r)
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
