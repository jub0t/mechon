// Package panel is the HTTP side of the mechon binary: the JSON API under /api/v1 and the embedded
// web UI for everything else.
package panel

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jub0t/mechon/internal/config"
	"github.com/jub0t/mechon/internal/db"
)

type Server struct {
	cfg  config.Panel
	pool *pgxpool.Pool
	q    *db.Queries
	web  fs.FS
	now  func() time.Time

	loginByIP    *limiter
	loginByEmail *limiter
}

func New(cfg config.Panel, pool *pgxpool.Pool, web fs.FS) *Server {
	return &Server{
		cfg:          cfg,
		pool:         pool,
		q:            db.New(pool),
		web:          web,
		now:          time.Now,
		loginByIP:    newLimiter(30, 15*time.Minute),
		loginByEmail: newLimiter(10, 15*time.Minute),
	}
}

func (s *Server) Handler() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/health", s.health)
	api.HandleFunc("POST /api/v1/auth/login", s.login)
	api.Handle("POST /api/v1/auth/logout", s.requireUser(s.logout))
	api.Handle("GET /api/v1/me", s.requireUser(s.me))
	api.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeError(w, r, errNotFound) })

	root := http.NewServeMux()
	root.Handle("/api/", s.checkOrigin(s.loadSession(api)))
	root.Handle("/", s.spa())
	return recoverer(logRequests(securityHeaders(root)))
}

// Background runs housekeeping until ctx is done.
func (s *Server) Background(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if n, err := s.q.DeleteExpiredSessions(ctx); err != nil {
				slog.Error("session cleanup", "err", err)
			} else if n > 0 {
				slog.Info("session cleanup", "deleted", n)
			}
			s.loginByIP.sweep(now)
			s.loginByEmail.sweep(now)
		}
	}
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.pool.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "database": "unreachable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
