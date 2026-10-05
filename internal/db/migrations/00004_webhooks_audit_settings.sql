-- +goose Up

CREATE TABLE webhook_endpoints (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    url         text NOT NULL,
    description text NOT NULL DEFAULT '',
    secret_enc  bytea NOT NULL,              -- HMAC signing secret, encrypted at rest
    events      text[] NOT NULL DEFAULT '{}', -- empty = every event
    enabled     bool NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TYPE webhook_delivery_status AS ENUM ('pending', 'delivered', 'failed');

CREATE TABLE webhook_deliveries (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    endpoint_id      uuid NOT NULL REFERENCES webhook_endpoints (id) ON DELETE CASCADE,
    event_id         uuid NOT NULL,
    event_type       text NOT NULL,
    payload          jsonb NOT NULL,
    status           webhook_delivery_status NOT NULL DEFAULT 'pending',
    attempts         int NOT NULL DEFAULT 0,
    last_status_code int,
    last_error       text NOT NULL DEFAULT '',
    created_at       timestamptz NOT NULL DEFAULT now(),
    delivered_at     timestamptz
);

CREATE INDEX webhook_deliveries_endpoint_idx ON webhook_deliveries (endpoint_id, created_at DESC);

CREATE TABLE audit_log (
    id          bigserial PRIMARY KEY,
    actor_id    uuid REFERENCES users (id) ON DELETE SET NULL,
    actor_name  text NOT NULL,             -- kept even if the account is deleted
    via_api_key bool NOT NULL DEFAULT false,
    action      text NOT NULL,             -- e.g. plan.create, bot.deploy
    target_type text NOT NULL,
    target_id   text NOT NULL,
    target_name text NOT NULL DEFAULT '',
    metadata    jsonb NOT NULL DEFAULT '{}',
    ip          text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_created_idx ON audit_log (created_at DESC);

-- Install-wide settings, one row per key.
CREATE TABLE settings (
    key        text PRIMARY KEY,
    value      text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE settings;
DROP TABLE audit_log;
DROP TABLE webhook_deliveries;
DROP TYPE webhook_delivery_status;
DROP TABLE webhook_endpoints;
