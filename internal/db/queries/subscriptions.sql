-- name: CreateSubscription :one
INSERT INTO subscriptions (user_id, plan_id, external_id) VALUES ($1, $2, $3) RETURNING *;

-- name: GetSubscription :one
SELECT * FROM subscriptions WHERE id = $1;

-- name: GetSubscriptionForUpdate :one
SELECT * FROM subscriptions WHERE id = $1 FOR UPDATE;

-- name: ListSubscriptionsForUser :many
SELECT sqlc.embed(subscriptions), sqlc.embed(plans),
       coalesce(sum(b.memory_mb) FILTER (WHERE b.id IS NOT NULL), 0)::int      AS used_memory_mb,
       coalesce(sum(b.cpu_millicores) FILTER (WHERE b.id IS NOT NULL), 0)::int AS used_cpu_millicores,
       coalesce(sum(b.disk_mb) FILTER (WHERE b.id IS NOT NULL), 0)::int        AS used_disk_mb,
       count(b.id)::int                                                       AS used_bots
FROM subscriptions
JOIN plans ON plans.id = subscriptions.plan_id
LEFT JOIN bots b ON b.subscription_id = subscriptions.id AND b.deleted_at IS NULL
WHERE subscriptions.user_id = $1 AND subscriptions.status <> 'terminated'
GROUP BY subscriptions.id, plans.id
ORDER BY subscriptions.created_at;

-- name: SubscriptionUsage :one
SELECT count(*)::int                           AS bots,
       coalesce(sum(memory_mb), 0)::int        AS memory_mb,
       coalesce(sum(cpu_millicores), 0)::int   AS cpu_millicores,
       coalesce(sum(disk_mb), 0)::int          AS disk_mb
FROM bots
WHERE subscription_id = @subscription_id AND deleted_at IS NULL AND id <> @exclude_bot_id;

-- name: SetSubscriptionStatus :one
UPDATE subscriptions SET status = $2 WHERE id = $1 RETURNING *;

-- name: SetSubscriptionPlan :one
UPDATE subscriptions SET plan_id = $2 WHERE id = $1 RETURNING *;

-- name: SetSubscriptionOverrides :one
UPDATE subscriptions
SET max_bots = $2, memory_mb = $3, cpu_millicores = $4, disk_mb = $5, pids_max = $6, note = $7
WHERE id = $1
RETURNING *;
