package panel

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jub0t/mechon/internal/auth"
	"github.com/jub0t/mechon/internal/db"
)

type userJSON struct {
	ID        uuid.UUID   `json:"id"`
	Email     string      `json:"email"`
	Name      string      `json:"name"`
	Role      db.UserRole `json:"role"`
	CreatedAt time.Time   `json:"createdAt"`
}

func toUserJSON(u db.User) userJSON {
	return userJSON{ID: u.ID, Email: u.Email, Name: u.Name, Role: u.Role, CreatedAt: u.CreatedAt}
}

var errBadCredentials = &apiError{http.StatusUnauthorized, "bad_credentials", "That email and password do not match."}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" || in.Password == "" {
		writeError(w, r, errBadRequest("Email and password are required."))
		return
	}
	if len(in.Password) > auth.MaxPasswordLen {
		writeError(w, r, errBadCredentials)
		return
	}
	now := s.now()
	ip := s.clientIP(r)
	if !s.loginByIP.allow(ip, now) || !s.loginByEmail.allow(email, now) {
		writeError(w, r, errRateLimited)
		return
	}

	user, err := s.q.GetUserByEmail(r.Context(), email)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && user.PasswordHash == nil) {
		auth.BurnVerify(in.Password)
		writeError(w, r, errBadCredentials)
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := auth.VerifyPassword(*user.PasswordHash, in.Password); err != nil {
		if !errors.Is(err, auth.ErrMismatch) {
			writeError(w, r, err)
			return
		}
		writeError(w, r, errBadCredentials)
		return
	}
	if user.SuspendedAt != nil {
		writeError(w, r, &apiError{http.StatusForbidden, "suspended", "This account is suspended. Contact your host."})
		return
	}

	token := auth.NewToken()
	exp := now.Add(sessionTTL)
	err = s.q.CreateSession(r.Context(), db.CreateSessionParams{
		TokenHash: auth.HashToken(token),
		UserID:    user.ID,
		ExpiresAt: exp,
		Ip:        ip,
		UserAgent: truncate(r.UserAgent(), 512),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	s.loginByEmail.reset(email)
	s.setCookie(w, token, exp)
	writeJSON(w, http.StatusOK, toUserJSON(user))
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.q.DeleteSession(r.Context(), currentSession(r).tokenHash); err != nil {
		writeError(w, r, err)
		return
	}
	s.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, toUserJSON(currentSession(r).user))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
