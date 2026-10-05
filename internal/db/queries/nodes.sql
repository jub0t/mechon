-- name: CreateNode :one
INSERT INTO nodes (name, region, token_hash, memory_mb, cpu_millicores, disk_mb, overcommit)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListNodes :many
SELECT n.*,
       count(b.id)::int                             AS bot_count,
       coalesce(sum(b.memory_mb), 0)::int           AS allocated_memory_mb,
       coalesce(sum(b.cpu_millicores), 0)::int      AS allocated_cpu_millicores,
       coalesce(sum(b.disk_mb), 0)::int             AS allocated_disk_mb
FROM nodes n
LEFT JOIN bots b ON b.node_id = n.id AND b.deleted_at IS NULL
GROUP BY n.id
ORDER BY n.name;

-- name: GetNode :one
SELECT * FROM nodes WHERE id = $1;

-- name: GetNodeByTokenHash :one
SELECT * FROM nodes WHERE token_hash = $1;

-- name: UpdateNodeHello :exec
UPDATE nodes SET agent_version = $2, info = $3, runsc = $4, last_seen_at = now() WHERE id = $1;

-- name: TouchNode :exec
UPDATE nodes SET last_seen_at = now() WHERE id = $1;

-- name: UpdateNode :one
UPDATE nodes SET name = $2, region = $3, memory_mb = $4, cpu_millicores = $5, disk_mb = $6,
                 overcommit = $7, maintenance = $8
WHERE id = $1
RETURNING *;

-- name: RotateNodeToken :exec
UPDATE nodes SET token_hash = $2 WHERE id = $1;

-- name: CountNodeBots :one
SELECT count(*)::int FROM bots WHERE node_id = $1 AND deleted_at IS NULL;

-- name: DeleteNode :exec
DELETE FROM nodes WHERE id = $1;

-- Candidates for placing a bot, most free memory first. Locks the rows so concurrent placements
-- serialise per node. Online means seen within the last 45 seconds.
-- name: PlacementCandidates :many
SELECT n.id, n.memory_mb, n.cpu_millicores, n.disk_mb, n.overcommit, n.runsc,
       coalesce((SELECT sum(b.memory_mb) FROM bots b WHERE b.node_id = n.id AND b.deleted_at IS NULL), 0)::int      AS used_memory_mb,
       coalesce((SELECT sum(b.cpu_millicores) FROM bots b WHERE b.node_id = n.id AND b.deleted_at IS NULL), 0)::int AS used_cpu_millicores,
       coalesce((SELECT sum(b.disk_mb) FROM bots b WHERE b.node_id = n.id AND b.deleted_at IS NULL), 0)::int        AS used_disk_mb
FROM nodes n
WHERE NOT n.maintenance
  AND n.last_seen_at > now() - interval '45 seconds'
  AND (NOT @need_runsc::bool OR n.runsc)
ORDER BY n.id
FOR UPDATE OF n;
