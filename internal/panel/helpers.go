package panel

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func pathID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		return uuid.Nil, errNotFound
	}
	return id, nil
}

// notFoundIfNoRows maps "no such row" to a 404 and leaves other errors alone.
func notFoundIfNoRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return errNotFound
	}
	return err
}

// uniqueViolation reports whether err is a Postgres unique-constraint failure.
func uniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

func errConflict(msg string) *apiError { return &apiError{http.StatusConflict, "conflict", msg} }

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

func normEmail(s string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(s))
	if len(e) > 254 || !emailRe.MatchString(e) {
		return "", errBadRequest("Enter a valid email address.")
	}
	return e, nil
}

func normName(s string, max int) (string, error) {
	n := strings.TrimSpace(s)
	if n == "" {
		return "", errBadRequest("Name is required.")
	}
	if len([]rune(n)) > max {
		return "", errBadRequest("Name is too long.")
	}
	return n, nil
}
