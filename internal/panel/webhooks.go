package panel

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/jub0t/mechon/internal/db"
)

// Webhooks tell an operator's billing system and tools what happened. Every event becomes one
// delivery row per subscribed endpoint plus a River job; the worker signs and POSTs it and River
// retries failures with exponential backoff for about a day.
//
// Requests carry:
//
//	Mechon-Event: bot.crashed
//	Mechon-Delivery: <delivery id>      (stable across retries; use it to de-duplicate)
//	Mechon-Signature: t=<unix seconds>,v1=<hex HMAC-SHA256 of "<t>.<body>" with the endpoint secret>
//
// and a JSON body {"id", "type", "createdAt", "data"}.

var webhookEvents = []string{
	"bot.created", "bot.deleted", "bot.crashed",
	"deploy.live", "deploy.failed",
	"subscription.created", "subscription.suspended", "subscription.activated", "subscription.terminated",
	"user.created", "user.suspended", "user.unsuspended",
	"node.online", "node.offline",
}

const webhookMaxAttempts = 14 // River backs off attempt^4 seconds: about 27 hours in all

type webhookArgs struct {
	DeliveryID uuid.UUID `json:"deliveryId"`
}

func (webhookArgs) Kind() string { return "webhook.deliver" }

func (webhookArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: webhookMaxAttempts}
}

type webhookWorker struct {
	river.WorkerDefaults[webhookArgs]
	s      *Server
	client *http.Client
}

func (w *webhookWorker) Timeout(*river.Job[webhookArgs]) time.Duration { return 30 * time.Second }

func (w *webhookWorker) Work(ctx context.Context, job *river.Job[webhookArgs]) error {
	q := w.s.q
	d, err := q.GetWebhookDelivery(ctx, job.Args.DeliveryID)
	if err != nil {
		return river.JobCancel(fmt.Errorf("delivery gone: %w", err))
	}
	if d.Status == db.WebhookDeliveryStatusDelivered {
		return nil
	}
	ep, err := q.GetWebhookEndpoint(ctx, d.EndpointID)
	if err != nil {
		return river.JobCancel(fmt.Errorf("endpoint gone: %w", err))
	}
	record := func(status db.WebhookDeliveryStatus, code *int32, msg string) {
		if err := q.RecordDeliveryAttempt(context.WithoutCancel(ctx), db.RecordDeliveryAttemptParams{ID: d.ID, Status: status, LastStatusCode: code, LastError: truncate(msg, 500)}); err != nil {
			slog.Error("webhook record", "delivery", d.ID, "err", err)
		}
	}
	if !ep.Enabled {
		record(db.WebhookDeliveryStatusFailed, nil, "endpoint is disabled")
		return river.JobCancel(fmt.Errorf("endpoint disabled"))
	}
	secret, err := w.s.box.Open(ep.SecretEnc, webhookAD(ep.ID))
	if err != nil {
		record(db.WebhookDeliveryStatusFailed, nil, "signing secret unreadable")
		return river.JobCancel(err)
	}

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.Url, bytes.NewReader(d.Payload))
	if err != nil {
		record(db.WebhookDeliveryStatusFailed, nil, err.Error())
		return river.JobCancel(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mechon-Webhooks/1")
	req.Header.Set("Mechon-Event", d.EventType)
	req.Header.Set("Mechon-Delivery", d.ID.String())
	req.Header.Set("Mechon-Signature", "t="+ts+",v1="+signWebhook(secret, ts, d.Payload))

	final := job.Attempt >= webhookMaxAttempts
	failStatus := db.WebhookDeliveryStatusPending
	if final {
		failStatus = db.WebhookDeliveryStatusFailed
	}
	res, err := w.client.Do(req)
	if err != nil {
		record(failStatus, nil, err.Error())
		return err
	}
	defer res.Body.Close()
	snippet, _ := io.ReadAll(io.LimitReader(res.Body, 300))
	code := int32(res.StatusCode)
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		record(db.WebhookDeliveryStatusDelivered, &code, "")
		return nil
	}
	msg := fmt.Sprintf("HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(snippet)))
	record(failStatus, &code, msg)
	return fmt.Errorf("%s", msg)
}

func signWebhook(secret []byte, ts string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func webhookAD(id uuid.UUID) []byte { return []byte("webhook/" + id.String()) }

// emit records an event for every endpoint that wants it and queues the deliveries. It runs after
// the change it describes has been committed, and never fails the request that caused it.
func (s *Server) emit(ctx context.Context, eventType string, data any) {
	ctx = context.WithoutCancel(ctx)
	eps, err := s.q.EndpointsForEvent(ctx, eventType)
	if err != nil {
		slog.Error("webhook endpoints", "event", eventType, "err", err)
		return
	}
	if len(eps) == 0 {
		return
	}
	s.deliver(ctx, eps, eventType, data)
}

func (s *Server) deliver(ctx context.Context, eps []db.WebhookEndpoint, eventType string, data any) {
	eventID := uuid.New()
	payload, err := json.Marshal(map[string]any{"id": eventID, "type": eventType, "createdAt": time.Now().UTC(), "data": data})
	if err != nil {
		slog.Error("webhook payload", "event", eventType, "err", err)
		return
	}
	for _, ep := range eps {
		d, err := s.q.CreateWebhookDelivery(ctx, db.CreateWebhookDeliveryParams{EndpointID: ep.ID, EventID: eventID, EventType: eventType, Payload: payload})
		if err != nil {
			slog.Error("webhook delivery", "event", eventType, "err", err)
			continue
		}
		if _, err := s.jobs.Insert(ctx, webhookArgs{DeliveryID: d.ID}, nil); err != nil {
			slog.Error("webhook enqueue", "event", eventType, "err", err)
		}
	}
}

// ---------- Admin API ----------

type webhookJSON struct {
	ID          uuid.UUID `json:"id"`
	URL         string    `json:"url"`
	Description string    `json:"description"`
	Events      []string  `json:"events"`
	Enabled     bool      `json:"enabled"`
	Failed24h   int32     `json:"failed24h"`
	CreatedAt   time.Time `json:"createdAt"`
}

func toWebhookJSON(e db.WebhookEndpoint, failed int32) webhookJSON {
	return webhookJSON{ID: e.ID, URL: e.Url, Description: e.Description, Events: e.Events, Enabled: e.Enabled, Failed24h: failed, CreatedAt: e.CreatedAt}
}

type webhookInput struct {
	URL         string   `json:"url"`
	Description string   `json:"description"`
	Events      []string `json:"events"`
	Enabled     *bool    `json:"enabled"`
}

func (in *webhookInput) validate() error {
	u, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return errBadRequest("Enter the full URL the events go to, e.g. https://billing.example.com/mechon.")
	}
	in.URL = u.String()
	in.Description = truncate(strings.TrimSpace(in.Description), 200)
	if in.Events == nil {
		in.Events = []string{}
	}
	for _, e := range in.Events {
		if !slices.Contains(webhookEvents, e) {
			return errBadRequest("Unknown event " + e + ".")
		}
	}
	return nil
}

func (s *Server) listWebhooks(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListWebhookEndpoints(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]webhookJSON, 0, len(rows))
	for _, e := range rows {
		out = append(out, toWebhookJSON(db.WebhookEndpoint{ID: e.ID, Url: e.Url, Description: e.Description, Events: e.Events, Enabled: e.Enabled, CreatedAt: e.CreatedAt}, e.Failed24h))
	}
	writeJSON(w, http.StatusOK, map[string]any{"endpoints": out, "events": webhookEvents})
}

// createWebhook returns the signing secret once.
func (s *Server) createWebhook(w http.ResponseWriter, r *http.Request) {
	var in webhookInput
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	if err := in.validate(); err != nil {
		writeError(w, r, err)
		return
	}
	raw := make([]byte, 24)
	_, _ = rand.Read(raw)
	secret := "whsec_" + hex.EncodeToString(raw)
	// The id is needed for the encryption binding, so create with a placeholder and seal after.
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer tx.Rollback(context.WithoutCancel(r.Context()))
	q := s.q.WithTx(tx)
	e, err := q.CreateWebhookEndpoint(r.Context(), db.CreateWebhookEndpointParams{Url: in.URL, Description: in.Description, SecretEnc: []byte{0}, Events: in.Events})
	if err != nil {
		writeError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), "UPDATE webhook_endpoints SET secret_enc = $2 WHERE id = $1", e.ID, s.box.Seal([]byte(secret), webhookAD(e.ID))); err != nil {
		writeError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, r, err)
		return
	}
	s.audit(r, "webhook.create", "webhook", e.ID.String(), e.Url, nil)
	writeJSON(w, http.StatusCreated, struct {
		webhookJSON
		Secret string `json:"secret"`
	}{toWebhookJSON(e, 0), secret})
}

func (s *Server) updateWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in webhookInput
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	if err := in.validate(); err != nil {
		writeError(w, r, err)
		return
	}
	enabled := in.Enabled == nil || *in.Enabled
	e, err := s.q.UpdateWebhookEndpoint(r.Context(), db.UpdateWebhookEndpointParams{ID: id, Url: in.URL, Description: in.Description, Events: in.Events, Enabled: enabled})
	if err != nil {
		writeError(w, r, notFoundIfNoRows(err))
		return
	}
	s.audit(r, "webhook.update", "webhook", e.ID.String(), e.Url, map[string]any{"enabled": e.Enabled, "events": e.Events})
	writeJSON(w, http.StatusOK, toWebhookJSON(e, 0))
}

func (s *Server) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	e, err := s.q.GetWebhookEndpoint(r.Context(), id)
	if err != nil {
		writeError(w, r, notFoundIfNoRows(err))
		return
	}
	if err := s.q.DeleteWebhookEndpoint(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	s.audit(r, "webhook.delete", "webhook", id.String(), e.Url, nil)
	w.WriteHeader(http.StatusNoContent)
}

// testWebhook sends a "ping" event to one endpoint.
func (s *Server) testWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	e, err := s.q.GetWebhookEndpoint(r.Context(), id)
	if err != nil {
		writeError(w, r, notFoundIfNoRows(err))
		return
	}
	s.deliver(context.WithoutCancel(r.Context()), []db.WebhookEndpoint{e}, "ping", map[string]any{"message": "Hello from Mechon. This endpoint is set up."})
	w.WriteHeader(http.StatusAccepted)
}

type deliveryJSON struct {
	ID             uuid.UUID                `json:"id"`
	EventType      string                   `json:"eventType"`
	Status         db.WebhookDeliveryStatus `json:"status"`
	Attempts       int32                    `json:"attempts"`
	LastStatusCode *int32                   `json:"lastStatusCode"`
	LastError      string                   `json:"lastError"`
	Payload        json.RawMessage          `json:"payload"`
	CreatedAt      time.Time                `json:"createdAt"`
	DeliveredAt    *time.Time               `json:"deliveredAt"`
}

func (s *Server) listDeliveries(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	rows, err := s.q.ListWebhookDeliveries(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]deliveryJSON, 0, len(rows))
	for _, d := range rows {
		out = append(out, deliveryJSON{d.ID, d.EventType, d.Status, d.Attempts, d.LastStatusCode, d.LastError, d.Payload, d.CreatedAt, d.DeliveredAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) retryDelivery(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, r, err)
		return
	}
	d, err := s.q.GetWebhookDelivery(r.Context(), id)
	if err != nil {
		writeError(w, r, notFoundIfNoRows(err))
		return
	}
	if err := s.q.ResetDelivery(r.Context(), d.ID); err != nil {
		writeError(w, r, err)
		return
	}
	if _, err := s.jobs.Insert(r.Context(), webhookArgs{DeliveryID: d.ID}, nil); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
