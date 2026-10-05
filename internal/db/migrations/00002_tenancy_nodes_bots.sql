-- +goose Up

-- ---------- Plans and subscriptions ----------

-- A plan is a pool: its limits are shared by every bot on a subscription to it.
CREATE TABLE plans (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug           text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9-]{0,62}$'),
    name           text NOT NULL,
    max_bots       int  NOT NULL CHECK (max_bots > 0),
    memory_mb      int  NOT NULL CHECK (memory_mb >= 64),
    cpu_millicores int  NOT NULL CHECK (cpu_millicores >= 50),
    disk_mb        int  NOT NULL CHECK (disk_mb >= 128),
    pids_max       int  NOT NULL DEFAULT 128 CHECK (pids_max BETWEEN 16 AND 4096),
    hardened       bool NOT NULL DEFAULT false,
    -- Templates this plan may use; empty means all.
    templates      text[] NOT NULL DEFAULT '{}',
    archived_at    timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TYPE subscription_status AS ENUM ('active', 'suspended', 'terminated');

CREATE TABLE subscriptions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    plan_id     uuid NOT NULL REFERENCES plans (id),
    status      subscription_status NOT NULL DEFAULT 'active',
    -- The billing system's service id, when it came from one.
    external_id text UNIQUE,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX subscriptions_user_id_idx ON subscriptions (user_id);

-- ---------- API keys ----------

CREATE TABLE api_keys (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         text NOT NULL,
    prefix       text NOT NULL,      -- first characters, shown in the UI to tell keys apart
    hash         bytea NOT NULL UNIQUE,
    scopes       text[] NOT NULL,
    last_used_at timestamptz,
    expires_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX api_keys_user_id_idx ON api_keys (user_id);

-- ---------- Nodes ----------

CREATE TABLE nodes (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name           text NOT NULL UNIQUE,
    region         text NOT NULL DEFAULT '',
    token_hash     bytea NOT NULL UNIQUE,
    -- Sellable capacity, set by the operator (not what the hardware reports).
    memory_mb      int NOT NULL CHECK (memory_mb > 0),
    cpu_millicores int NOT NULL CHECK (cpu_millicores > 0),
    disk_mb        int NOT NULL CHECK (disk_mb > 0),
    overcommit     numeric(4, 2) NOT NULL DEFAULT 1.0 CHECK (overcommit BETWEEN 1.0 AND 10.0),
    maintenance    bool NOT NULL DEFAULT false,
    -- Filled in from the agent's hello.
    agent_version  text NOT NULL DEFAULT '',
    info           jsonb NOT NULL DEFAULT '{}',
    runsc          bool NOT NULL DEFAULT false,
    last_seen_at   timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now()
);

-- ---------- Bots ----------

CREATE SEQUENCE bot_uid_seq START 1;

CREATE TYPE bot_desired_state AS ENUM ('running', 'stopped');
CREATE TYPE bot_observed_state AS ENUM ('pending', 'installing', 'running', 'stopped', 'crashed', 'unknown');

CREATE TABLE bots (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The bot's Linux UID is 100000 + uid_seq; never reused.
    uid_seq           int NOT NULL UNIQUE DEFAULT nextval('bot_uid_seq'),
    subscription_id   uuid NOT NULL REFERENCES subscriptions (id),
    node_id           uuid NOT NULL REFERENCES nodes (id),
    name              text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 48),
    template          text NOT NULL,
    memory_mb         int NOT NULL CHECK (memory_mb >= 64),
    cpu_millicores    int NOT NULL CHECK (cpu_millicores >= 50),
    disk_mb           int NOT NULL CHECK (disk_mb >= 128),
    desired_state     bot_desired_state NOT NULL DEFAULT 'running',
    observed_state    bot_observed_state NOT NULL DEFAULT 'pending',
    observed_error    text NOT NULL DEFAULT '',
    exit_code         int,
    current_deploy_id uuid,
    restart_count     int NOT NULL DEFAULT 0,
    state_changed_at  timestamptz NOT NULL DEFAULT now(),
    created_at        timestamptz NOT NULL DEFAULT now(),
    deleted_at        timestamptz
);

CREATE INDEX bots_subscription_id_idx ON bots (subscription_id) WHERE deleted_at IS NULL;
CREATE INDEX bots_node_id_idx ON bots (node_id) WHERE deleted_at IS NULL;

-- ---------- Deploys ----------

CREATE TYPE deploy_source AS ENUM ('upload', 'git', 'api', 'rollback');
CREATE TYPE deploy_status AS ENUM ('queued', 'fetching', 'installing', 'live', 'failed', 'superseded');

CREATE TABLE deploys (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    bot_id          uuid NOT NULL REFERENCES bots (id) ON DELETE CASCADE,
    number          int NOT NULL,
    source          deploy_source NOT NULL,
    git_url         text NOT NULL DEFAULT '',
    git_ref         text NOT NULL DEFAULT '',
    git_commit      text NOT NULL DEFAULT '',
    -- For rollbacks: the deploy whose artifact this one reuses.
    rollback_of     uuid REFERENCES deploys (id),
    artifact_sha256 text NOT NULL,
    artifact_bytes  bigint NOT NULL,
    status          deploy_status NOT NULL DEFAULT 'queued',
    error           text NOT NULL DEFAULT '',
    log             text NOT NULL DEFAULT '',
    created_by      uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    finished_at     timestamptz,
    UNIQUE (bot_id, number)
);

ALTER TABLE bots ADD CONSTRAINT bots_current_deploy_fk FOREIGN KEY (current_deploy_id) REFERENCES deploys (id) ON DELETE SET NULL;

-- ---------- Environment ----------

CREATE TABLE bot_env (
    bot_id    uuid NOT NULL REFERENCES bots (id) ON DELETE CASCADE,
    key       text NOT NULL CHECK (key ~ '^[A-Za-z_][A-Za-z0-9_]{0,127}$'),
    value_enc bytea NOT NULL, -- AES-256-GCM with MECHON_SECRET_KEY
    secret    bool NOT NULL DEFAULT false,
    PRIMARY KEY (bot_id, key)
);

-- ---------- Metrics (one row per bot per minute) ----------

CREATE TABLE bot_metrics (
    bot_id       uuid NOT NULL REFERENCES bots (id) ON DELETE CASCADE,
    ts           timestamptz NOT NULL,
    cpu_pct      real NOT NULL,
    memory_bytes bigint NOT NULL,
    disk_bytes   bigint NOT NULL,
    net_rx       bigint NOT NULL,
    net_tx       bigint NOT NULL,
    PRIMARY KEY (bot_id, ts)
);

-- +goose Down
DROP TABLE bot_metrics;
DROP TABLE bot_env;
ALTER TABLE bots DROP CONSTRAINT bots_current_deploy_fk;
DROP TABLE deploys;
DROP TYPE deploy_status;
DROP TYPE deploy_source;
DROP TABLE bots;
DROP TYPE bot_observed_state;
DROP TYPE bot_desired_state;
DROP SEQUENCE bot_uid_seq;
DROP TABLE nodes;
DROP TABLE api_keys;
DROP TABLE subscriptions;
DROP TYPE subscription_status;
DROP TABLE plans;
