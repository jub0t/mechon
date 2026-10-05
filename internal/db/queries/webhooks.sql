-- name: ListWebhookEndpoints :many
SELECT e.*,
       (SELECT count(*) FROM webhook_deliveries d WHERE d.endpoint_id = e.id AND d.status = 'failed' AND d.created_at > now() - interval '24 hours')::int AS failed_24h
FROM webhook_endpoints e
ORDER BY e.created_at;

-- name: GetWebhookEndpoint :one
SELECT * FROM webhook_endpoints WHERE id = $1;

-- name: CreateWebhookEndpoint :one
INSERT INTO webhook_endpoints (url, description, secret_enc, events) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: UpdateWebhookEndpoint :one
UPDATE webhook_endpoints SET url = $2, description = $3, events = $4, enabled = $5 WHERE id = $1 RETURNING *;

-- name: DeleteWebhookEndpoint :exec
DELETE FROM webhook_endpoints WHERE id = $1;

-- name: EndpointsForEvent :many
SELECT * FROM webhook_endpoints WHERE enabled AND (events = '{}' OR @event_type::text = ANY (events));

-- name: CreateWebhookDelivery :one
INSERT INTO webhook_deliveries (endpoint_id, event_id, event_type, payload) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: GetWebhookDelivery :one
SELECT * FROM webhook_deliveries WHERE id = $1;

-- name: ListWebhookDeliveries :many
SELECT * FROM webhook_deliveries WHERE endpoint_id = $1 ORDER BY created_at DESC LIMIT 50;

-- name: RecordDeliveryAttempt :exec
UPDATE webhook_deliveries
SET attempts = attempts + 1, status = @status::webhook_delivery_status, last_status_code = @last_status_code,
    last_error = @last_error, delivered_at = CASE WHEN @status::webhook_delivery_status = 'delivered' THEN now() ELSE delivered_at END
WHERE id = @id;

-- name: ResetDelivery :exec
UPDATE webhook_deliveries SET status = 'pending' WHERE id = $1;
