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
	ID          uuid.UUID   `json:"id"`
	Email       string      `json:"email"`
	Name        string      `json:"name"`
	Role        db.UserRole `json:"role"`
	ExternalID  *string     `json:"externalId"`
	SuspendedAt *time.Time  `json:"suspendedAt"`
	HasPassword bool        `json:"hasPassword"`
	CreatedAt   time.Time   `json:"createdAt"`
}

func toUserJSON(u db.User) userJSON {
	return userJSON{
		ID: u.ID, Email: u.Email, Name: u.Name, Role: u.Role, ExternalID: u.ExternalID,
		SuspendedAt: u.SuspendedAt, HasPassword: u.PasswordHash != nil, CreatedAt: u.CreatedAt,
	}
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
	if err := s.q.DeleteSession(r.Context(), currentPrincipal(r).tokenHash); err != nil {
		writeError(w, r, err)
		return
	}
	s.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, toUserJSON(currentPrincipal(r).user))
}

func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		Email string `json:"email"`
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
	u, err := s.q.UpdateUserProfile(r.Context(), db.UpdateUserProfileParams{ID: currentPrincipal(r).user.ID, Name: name, Email: email})
	if uniqueViolation(err) {
		writeError(w, r, errConflict("Another account already uses that email."))
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toUserJSON(u))
}

func checkNewPassword(p string) error {
	if len(p) < auth.MinPasswordLen {
		return errBadRequest("Use at least 10 characters for the password.")
	}
	if len(p) > auth.MaxPasswordLen {
		return errBadRequest("That password is too long.")
	}
	return nil
}

// changePassword needs the current password and signs out every other session.
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	p := currentPrincipal(r)
	if !s.loginByEmail.allow("pw:"+p.user.ID.String(), s.now()) {
		writeError(w, r, errRateLimited)
		return
	}
	if p.user.PasswordHash == nil || auth.VerifyPassword(*p.user.PasswordHash, in.Current) != nil {
		writeError(w, r, &apiError{http.StatusBadRequest, "bad_credentials", "Your current password is not right."})
		return
	}
	if err := checkNewPassword(in.New); err != nil {
		writeError(w, r, err)
		return
	}
	hash, err := auth.HashPassword(in.New)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.q.SetUserPassword(r.Context(), db.SetUserPasswordParams{ID: p.user.ID, PasswordHash: &hash}); err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.q.DeleteUserSessions(r.Context(), db.DeleteUserSessionsParams{UserID: p.user.ID, TokenHash: p.tokenHash}); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
