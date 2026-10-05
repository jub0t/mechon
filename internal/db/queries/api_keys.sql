-- name: CreateAPIKey :one
INSERT INTO api_keys (user_id, name, prefix, hash, scopes, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListAPIKeys :many
SELECT * FROM api_keys WHERE user_id = $1 ORDER BY created_at DESC;

-- name: DeleteAPIKey :execrows
DELETE FROM api_keys WHERE id = $1 AND user_id = $2;

-- name: GetAPIKeyUser :one
SELECT sqlc.embed(users), api_keys.id AS key_id, api_keys.scopes, api_keys.last_used_at
FROM api_keys
JOIN users ON users.id = api_keys.user_id
WHERE api_keys.hash = $1 AND (api_keys.expires_at IS NULL OR api_keys.expires_at > now());

-- name: TouchAPIKey :exec
UPDATE api_keys SET last_used_at = now() WHERE id = $1;
