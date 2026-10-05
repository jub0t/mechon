-- name: InsertAudit :exec
INSERT INTO audit_log (actor_id, actor_name, via_api_key, action, target_type, target_id, target_name, metadata, ip)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: ListAudit :many
SELECT * FROM audit_log
WHERE (sqlc.narg(before)::bigint IS NULL OR id < sqlc.narg(before))
  AND (@prefix::text = '' OR action LIKE @prefix::text || '%')
ORDER BY id DESC
LIMIT 100;

-- name: GetSetting :one
SELECT value FROM settings WHERE key = $1;

-- name: ListSettings :many
SELECT key, value FROM settings;

-- name: PutSetting :exec
INSERT INTO settings (key, value) VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = now();
