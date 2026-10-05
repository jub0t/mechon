-- name: CreateUser :one
INSERT INTO users (email, name, password_hash, role)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: CountAdmins :one
SELECT count(*) FROM users WHERE role = 'admin';

-- name: ListUsers :many
SELECT u.*,
       (SELECT count(*) FROM subscriptions s WHERE s.user_id = u.id AND s.status <> 'terminated')::int AS subscription_count,
       (SELECT count(*) FROM bots b JOIN subscriptions s ON s.id = b.subscription_id
         WHERE s.user_id = u.id AND b.deleted_at IS NULL)::int AS bot_count
FROM users u
ORDER BY u.role, u.created_at;

-- name: CreateUserFull :one
INSERT INTO users (email, name, password_hash, role, external_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateUserProfile :one
UPDATE users SET name = $2, email = $3 WHERE id = $1 RETURNING *;

-- name: UpdateUserRole :one
UPDATE users SET role = $2 WHERE id = $1 RETURNING *;

-- name: SetUserSuspended :one
UPDATE users SET suspended_at = CASE WHEN @suspended::bool THEN coalesce(suspended_at, now()) ELSE NULL END
WHERE id = @id RETURNING *;

-- name: SetUserPassword :exec
UPDATE users SET password_hash = $2 WHERE id = $1;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = $1 AND token_hash <> $2;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = $1;
