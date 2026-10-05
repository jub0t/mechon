-- name: ListPlans :many
SELECT p.*,
       (SELECT count(*) FROM subscriptions s WHERE s.plan_id = p.id AND s.status <> 'terminated')::int AS subscription_count
FROM plans p
WHERE p.archived_at IS NULL
ORDER BY p.memory_mb, p.name;

-- name: GetPlan :one
SELECT * FROM plans WHERE id = $1;

-- name: CreatePlan :one
INSERT INTO plans (slug, name, max_bots, memory_mb, cpu_millicores, disk_mb, pids_max, hardened, templates)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: UpdatePlan :one
UPDATE plans SET name = $2, max_bots = $3, memory_mb = $4, cpu_millicores = $5, disk_mb = $6,
                 pids_max = $7, hardened = $8, templates = $9
WHERE id = $1 AND archived_at IS NULL
RETURNING *;

-- name: ArchivePlan :exec
UPDATE plans SET archived_at = now() WHERE id = $1;
