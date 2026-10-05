package panel

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
)

// apiError is the one error shape the API returns: {"error": {"code": "...", "message": "..."}}.
type apiError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *apiError) Error() string { return e.Message }

func errBadRequest(msg string) *apiError { return &apiError{http.StatusBadRequest, "bad_request", msg} }

var (
	errUnauthorized = &apiError{http.StatusUnauthorized, "unauthorized", "Sign in to continue."}
	errForbidden    = &apiError{http.StatusForbidden, "forbidden", "You do not have access to this."}
	errNotFound     = &apiError{http.StatusNotFound, "not_found", "Not found."}
	errBadOrigin    = &apiError{http.StatusForbidden, "bad_origin", "Request origin is not allowed."}
	errRateLimited  = &apiError{http.StatusTooManyRequests, "rate_limited", "Too many attempts. Try again in a few minutes."}
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError reports an *apiError as is and anything else as a 500, logging the cause.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apiError
	if !errors.As(err, &ae) {
		slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		ae = &apiError{http.StatusInternalServerError, "internal", "Something went wrong on our side."}
	}
	writeJSON(w, ae.Status, map[string]any{"error": ae})
}

const maxBody = 1 << 20

// decode reads a JSON body into v, rejecting unknown fields, trailing data and oversized bodies.
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errBadRequest("Request body is not valid JSON for this endpoint.")
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return errBadRequest("Request body has trailing data.")
	}
	return nil
}
