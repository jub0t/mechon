-- name: ListBotEnv :many
SELECT * FROM bot_env WHERE bot_id = $1 ORDER BY key;

-- name: DeleteBotEnv :exec
DELETE FROM bot_env WHERE bot_id = $1;

-- name: InsertBotEnv :exec
INSERT INTO bot_env (bot_id, key, value_enc, secret) VALUES ($1, $2, $3, $4);
