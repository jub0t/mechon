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
