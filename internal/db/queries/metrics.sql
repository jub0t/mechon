-- name: UpsertBotMetric :exec
INSERT INTO bot_metrics (bot_id, ts, cpu_pct, memory_bytes, disk_bytes, net_rx, net_tx)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (bot_id, ts) DO UPDATE
SET cpu_pct = excluded.cpu_pct, memory_bytes = excluded.memory_bytes, disk_bytes = excluded.disk_bytes,
    net_rx = excluded.net_rx, net_tx = excluded.net_tx;

-- name: ListBotMetrics :many
SELECT ts, cpu_pct, memory_bytes, disk_bytes FROM bot_metrics
WHERE bot_id = $1 AND ts >= $2
ORDER BY ts;

-- name: DeleteOldMetrics :execrows
DELETE FROM bot_metrics WHERE ts < $1;

-- Every bot's usage added up, per minute.
-- name: FleetMetrics :many
SELECT m.ts,
       sum(m.memory_bytes)::bigint                          AS memory_bytes,
       sum(m.cpu_pct * b.cpu_millicores / 100000.0)::float8 AS cpu_cores,
       count(*)::int                                        AS bots
FROM bot_metrics m
JOIN bots b ON b.id = m.bot_id
WHERE m.ts >= $1
GROUP BY m.ts
ORDER BY m.ts;

-- name: DeploysPerDay :many
SELECT date_trunc('day', created_at)::timestamptz                                AS day,
       count(*) FILTER (WHERE status IN ('live', 'superseded'))::int            AS succeeded,
       count(*) FILTER (WHERE status = 'failed')::int                           AS failed
FROM deploys
WHERE created_at >= $1
GROUP BY 1
ORDER BY 1;
