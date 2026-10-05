-- name: CreateBot :one
INSERT INTO bots (subscription_id, node_id, name, template, memory_mb, cpu_millicores, disk_mb)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetBot :one
SELECT sqlc.embed(bots), subscriptions.user_id AS owner_id, subscriptions.status AS subscription_status,
       plans.pids_max, plans.hardened, plans.name AS plan_name, nodes.name AS node_name,
       users.name AS owner_name, users.email AS owner_email
FROM bots
JOIN subscriptions ON subscriptions.id = bots.subscription_id
JOIN users ON users.id = subscriptions.user_id
JOIN plans ON plans.id = subscriptions.plan_id
JOIN nodes ON nodes.id = bots.node_id
WHERE bots.id = $1 AND bots.deleted_at IS NULL;

-- name: ListBots :many
SELECT sqlc.embed(bots), subscriptions.user_id AS owner_id, users.name AS owner_name, users.email AS owner_email,
       nodes.name AS node_name, plans.name AS plan_name
FROM bots
JOIN subscriptions ON subscriptions.id = bots.subscription_id
JOIN users ON users.id = subscriptions.user_id
JOIN plans ON plans.id = subscriptions.plan_id
JOIN nodes ON nodes.id = bots.node_id
WHERE bots.deleted_at IS NULL
  AND (sqlc.narg(owner_id)::uuid IS NULL OR subscriptions.user_id = sqlc.narg(owner_id))
ORDER BY bots.created_at DESC;

-- Everything the panel needs to build the specs for one node's sync.
-- name: ListNodeBotSpecs :many
SELECT sqlc.embed(bots), plans.pids_max, plans.hardened, subscriptions.status AS subscription_status,
       (users.suspended_at IS NOT NULL)::bool AS owner_suspended, deploys.artifact_sha256
FROM bots
JOIN subscriptions ON subscriptions.id = bots.subscription_id
JOIN users ON users.id = subscriptions.user_id
JOIN plans ON plans.id = subscriptions.plan_id
LEFT JOIN deploys ON deploys.id = bots.current_deploy_id
WHERE bots.node_id = $1 AND bots.deleted_at IS NULL;

-- name: GetBotSpec :one
SELECT sqlc.embed(bots), plans.pids_max, plans.hardened, subscriptions.status AS subscription_status,
       (users.suspended_at IS NOT NULL)::bool AS owner_suspended, deploys.artifact_sha256
FROM bots
JOIN subscriptions ON subscriptions.id = bots.subscription_id
JOIN users ON users.id = subscriptions.user_id
JOIN plans ON plans.id = subscriptions.plan_id
LEFT JOIN deploys ON deploys.id = bots.current_deploy_id
WHERE bots.id = $1;

-- name: ListUserBotIDs :many
SELECT bots.id, bots.node_id FROM bots
JOIN subscriptions ON subscriptions.id = bots.subscription_id
WHERE subscriptions.user_id = $1 AND bots.deleted_at IS NULL;

-- name: SetBotDesired :exec
UPDATE bots SET desired_state = $2 WHERE id = $1;

-- name: SetBotsDesiredForSubscription :many
UPDATE bots SET desired_state = $2 WHERE subscription_id = $1 AND deleted_at IS NULL RETURNING id, node_id;

-- name: SetBotObserved :exec
UPDATE bots SET observed_state = @observed_state::bot_observed_state, observed_error = @observed_error,
                exit_code = @exit_code, restart_count = @restart_count,
                state_changed_at = CASE WHEN observed_state <> @observed_state::bot_observed_state THEN now() ELSE state_changed_at END
WHERE id = @id;

-- name: MarkNodeBotsUnknown :exec
UPDATE bots SET observed_state = 'unknown', state_changed_at = now()
WHERE node_id = $1 AND deleted_at IS NULL AND observed_state <> 'unknown';

-- name: UpdateBotSettings :one
UPDATE bots SET name = $2, memory_mb = $3, cpu_millicores = $4, disk_mb = $5
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: SetBotCurrentDeploy :exec
UPDATE bots SET current_deploy_id = $2 WHERE id = $1;

-- name: SoftDeleteBot :exec
UPDATE bots SET deleted_at = now() WHERE id = $1;

-- name: CountBotsByState :many
SELECT observed_state, count(*)::int AS n FROM bots WHERE deleted_at IS NULL GROUP BY observed_state;

-- name: ListSubscriptionBotIDs :many
SELECT id FROM bots WHERE subscription_id = $1 AND deleted_at IS NULL;

-- name: ListPlanBotIDs :many
SELECT bots.id FROM bots JOIN subscriptions ON subscriptions.id = bots.subscription_id
WHERE subscriptions.plan_id = $1 AND bots.deleted_at IS NULL;
