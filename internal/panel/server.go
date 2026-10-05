// Package panel is the HTTP side of the mechon binary: the JSON API under /api/v1, the agent
// endpoints under /agent/v1, and the embedded web UI for everything else.
package panel

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/jub0t/mechon/internal/config"
	"github.com/jub0t/mechon/internal/db"
	"github.com/jub0t/mechon/internal/secrets"
)

type Server struct {
	cfg  config.Panel
	pool *pgxpool.Pool
	q    *db.Queries
	web  fs.FS
	now  func() time.Time
	box  *secrets.Box
	hub  *Hub
	jobs *river.Client[pgx.Tx]

	artifactDir string

	loginByIP    *limiter
	loginByEmail *limiter
	apiByIP      *limiter
}

func New(cfg config.Panel, pool *pgxpool.Pool, web fs.FS) (*Server, error) {
	box, err := secrets.Open(cfg.SecretKey)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(cfg.DataDir, "artifacts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	s := &Server{
		cfg:          cfg,
		pool:         pool,
		q:            db.New(pool),
		web:          web,
		now:          time.Now,
		box:          box,
		artifactDir:  dir,
		loginByIP:    newLimiter(30, 15*time.Minute),
		loginByEmail: newLimiter(10, 15*time.Minute),
		apiByIP:      newLimiter(1200, time.Minute),
	}
	s.hub = newHub(s)

	workers := river.NewWorkers()
	river.AddWorker(workers, &webhookWorker{s: s, client: &http.Client{Timeout: 15 * time.Second}})
	jobs, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues:  map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 8}},
		Workers: workers,
		Logger:  slog.New(slog.DiscardHandler),
	})
	if err != nil {
		return nil, fmt.Errorf("job queue: %w", err)
	}
	s.jobs = jobs
	return s, nil
}

// StartJobs runs background jobs (webhook deliveries) until ctx is done.
func (s *Server) StartJobs(ctx context.Context) error {
	if err := s.jobs.Start(ctx); err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.jobs.Stop(stopCtx)
	}()
	return nil
}

func (s *Server) Handler() http.Handler {
	read := access{scope: ScopeRead}
	write := access{scope: ScopeWrite}
	admin := access{scope: ScopeOperator}
	signedIn := access{}
	session := access{sessionOnly: true}

	api := http.NewServeMux()
	route := func(pattern string, a access, h http.HandlerFunc) { api.Handle(pattern, s.guard(a, h)) }

	api.HandleFunc("GET /api/v1/health", s.health)
	api.HandleFunc("POST /api/v1/auth/login", s.login)
	route("POST /api/v1/auth/logout", session, s.logout)

	route("GET /api/v1/me", signedIn, s.me)
	route("PATCH /api/v1/me", session, s.updateMe)
	route("POST /api/v1/me/password", session, s.changePassword)
	route("GET /api/v1/me/subscriptions", read, s.mySubscriptions)
	route("GET /api/v1/me/keys", session, s.listKeys)
	route("POST /api/v1/me/keys", session, s.createKey)
	route("DELETE /api/v1/me/keys/{id}", session, s.deleteKey)

	route("GET /api/v1/templates", signedIn, s.listTemplates)
	api.HandleFunc("GET /api/v1/settings/public", s.publicSettings)
	route("PUT /api/v1/settings", admin, s.updateSettings)
	route("GET /api/v1/audit", admin, s.listAudit)
	route("GET /api/v1/webhooks", admin, s.listWebhooks)
	route("POST /api/v1/webhooks", admin, s.createWebhook)
	route("PATCH /api/v1/webhooks/{id}", admin, s.updateWebhook)
	route("DELETE /api/v1/webhooks/{id}", admin, s.deleteWebhook)
	route("POST /api/v1/webhooks/{id}/test", admin, s.testWebhook)
	route("GET /api/v1/webhooks/{id}/deliveries", admin, s.listDeliveries)
	route("POST /api/v1/webhook-deliveries/{id}/retry", admin, s.retryDelivery)
	route("GET /api/v1/overview", admin, s.overview)
	route("GET /api/v1/overview/activity", admin, s.activity)

	route("GET /api/v1/plans", admin, s.listPlans)
	route("POST /api/v1/plans", admin, s.createPlan)
	route("PATCH /api/v1/plans/{id}", admin, s.updatePlan)
	route("DELETE /api/v1/plans/{id}", admin, s.archivePlan)

	route("GET /api/v1/users", admin, s.listUsers)
	route("POST /api/v1/users", admin, s.createUser)
	route("GET /api/v1/users/{id}", admin, s.getUser)
	route("PATCH /api/v1/users/{id}", admin, s.updateUser)
	route("DELETE /api/v1/users/{id}", admin, s.deleteUser)
	route("POST /api/v1/users/{id}/password", admin, s.resetUserPassword)
	route("POST /api/v1/users/{id}/suspend", admin, s.suspendUser)
	route("GET /api/v1/users/{id}/subscriptions", admin, s.userSubscriptions)
	route("POST /api/v1/users/{id}/subscriptions", admin, s.createSubscription)
	route("PATCH /api/v1/subscriptions/{id}", admin, s.updateSubscription)

	route("GET /api/v1/nodes", admin, s.listNodes)
	route("POST /api/v1/nodes", admin, s.createNode)
	route("PATCH /api/v1/nodes/{id}", admin, s.updateNode)
	route("DELETE /api/v1/nodes/{id}", admin, s.deleteNode)
	route("POST /api/v1/nodes/{id}/token", admin, s.rotateNodeToken)

	route("GET /api/v1/bots", read, s.listBots)
	route("POST /api/v1/bots", write, s.createBot)
	route("GET /api/v1/bots/{id}", read, s.getBot)
	route("PATCH /api/v1/bots/{id}", write, s.updateBot)
	route("DELETE /api/v1/bots/{id}", write, s.deleteBot)
	route("POST /api/v1/bots/{id}/actions", write, s.botAction)
	route("GET /api/v1/bots/{id}/env", read, s.getEnv)
	route("PUT /api/v1/bots/{id}/env", write, s.putEnv)
	route("GET /api/v1/bots/{id}/metrics", read, s.botMetrics)
	route("GET /api/v1/bots/{id}/stream", read, s.botStream)
	route("GET /api/v1/bots/{id}/files", read, s.listFiles)
	route("GET /api/v1/bots/{id}/files/content", read, s.readFile)
	route("PUT /api/v1/bots/{id}/files/content", write, s.writeFile)
	route("DELETE /api/v1/bots/{id}/files", write, s.deleteFile)
	route("POST /api/v1/bots/{id}/files/dir", write, s.makeDir)
	route("GET /api/v1/bots/{id}/deploys", read, s.listDeploys)
	route("POST /api/v1/bots/{id}/deploys", write, s.createDeploy)
	route("GET /api/v1/bots/{id}/deploys/{deploy}", read, s.getDeploy)
	route("POST /api/v1/bots/{id}/deploys/{deploy}/rollback", write, s.rollbackDeploy)

	api.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeError(w, r, errNotFound) })

	root := http.NewServeMux()
	root.Handle("/api/", s.rateLimitAPI(s.checkOrigin(s.loadPrincipal(api))))
	root.HandleFunc("GET /agent/v1/connect", s.hub.serveAgent)
	root.HandleFunc("GET /agent/v1/artifacts/{id}", s.serveArtifact)
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
			if _, err := s.q.DeleteOldMetrics(ctx, now.Add(-14*24*time.Hour)); err != nil {
				slog.Error("metrics cleanup", "err", err)
			}
			s.loginByIP.sweep(now)
			s.loginByEmail.sweep(now)
			s.apiByIP.sweep(now)
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
