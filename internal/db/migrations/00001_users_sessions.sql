-- +goose Up
CREATE TYPE user_role AS ENUM ('admin', 'user');

CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text NOT NULL UNIQUE CHECK (email = lower(email)),
    name          text NOT NULL,
    -- Null for accounts a billing system created that have not set a password yet.
    password_hash text,
    role          user_role NOT NULL DEFAULT 'user',
    -- The billing system's customer id, when the account came from one.
    external_id   text UNIQUE,
    suspended_at  timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    -- sha256 of the cookie token; the token itself is never stored.
    token_hash   bytea PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at   timestamptz NOT NULL,
    ip           text NOT NULL,
    user_agent   text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- +goose Down
DROP TABLE sessions;
DROP TABLE users;
DROP TYPE user_role;
