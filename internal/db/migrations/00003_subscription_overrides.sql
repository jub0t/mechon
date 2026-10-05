-- +goose Up
-- Per-customer overrides of their plan's limits. NULL means "use the plan".
ALTER TABLE subscriptions
    ADD COLUMN max_bots       int CHECK (max_bots > 0),
    ADD COLUMN memory_mb      int CHECK (memory_mb >= 64),
    ADD COLUMN cpu_millicores int CHECK (cpu_millicores >= 50),
    ADD COLUMN disk_mb        int CHECK (disk_mb >= 128),
    ADD COLUMN pids_max       int CHECK (pids_max BETWEEN 16 AND 4096),
    ADD COLUMN note           text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE subscriptions
    DROP COLUMN max_bots, DROP COLUMN memory_mb, DROP COLUMN cpu_millicores,
    DROP COLUMN disk_mb, DROP COLUMN pids_max, DROP COLUMN note;
