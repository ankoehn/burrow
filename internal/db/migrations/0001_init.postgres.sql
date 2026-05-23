-- internal/db/migrations/0001_init.postgres.sql
-- +goose Up
CREATE TABLE users (
    id            TEXT PRIMARY KEY,        -- uuid
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,           -- argon2id encoded
    role          TEXT NOT NULL DEFAULT 'admin',  -- 'admin' or 'user'
    created_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,           -- random 32 bytes, hex
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    user_agent TEXT,
    ip         TEXT
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

CREATE TABLE client_tokens (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    token_hash  TEXT NOT NULL UNIQUE,       -- sha256 of the actual token
    last_used   TIMESTAMP WITH TIME ZONE,
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_tokens_user ON client_tokens(user_id);

-- Tunnel records are persisted so the dashboard can show history,
-- but the live state lives in-memory in the server.
CREATE TABLE tunnels (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    type         TEXT NOT NULL,             -- 'tcp' for MVP
    remote_port  INTEGER NOT NULL,
    local_addr   TEXT NOT NULL,
    created_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    last_seen    TIMESTAMP WITH TIME ZONE
);
CREATE INDEX idx_tunnels_user ON tunnels(user_id);

-- +goose Down
DROP TABLE tunnels;
DROP TABLE client_tokens;
DROP TABLE sessions;
DROP TABLE users;
