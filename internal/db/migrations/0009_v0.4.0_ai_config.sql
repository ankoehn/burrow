-- internal/db/migrations/0009_v0.4.0_ai_config.sql
-- +goose Up
CREATE TABLE service_ai_config (
    service_id  TEXT PRIMARY KEY REFERENCES services(id) ON DELETE CASCADE,
    config      TEXT NOT NULL DEFAULT '{}', -- ServiceAIConfig JSON (spec Part B.7)
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE model_aliases (
    alias          TEXT PRIMARY KEY,
    concrete_model TEXT NOT NULL,
    service_id     TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE rate_limits (
    id          TEXT PRIMARY KEY,
    scope       TEXT NOT NULL,                -- api_key|role|service|global
    subject     TEXT NOT NULL DEFAULT '',
    dimension   TEXT NOT NULL,                -- rpm|bpm
    lim         INTEGER NOT NULL,
    burst       INTEGER NOT NULL,
    "window"    TEXT NOT NULL DEFAULT 'minute', -- minute|day
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX rate_limits_scope_subject ON rate_limits(scope, subject);
CREATE TABLE budgets (
    id                TEXT PRIMARY KEY,
    scope             TEXT NOT NULL,         -- api_key|service|user|global
    subject_id        TEXT NOT NULL DEFAULT '',
    daily_usd         REAL NOT NULL,
    action_on_exceed  TEXT NOT NULL,         -- alert_webhook|throttle_zero|disable_key
    alert_webhook_id  TEXT,
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE service_ip_geo (
    service_id      TEXT PRIMARY KEY REFERENCES services(id) ON DELETE CASCADE,
    enabled         INTEGER NOT NULL DEFAULT 0,
    allow_cidrs     TEXT NOT NULL DEFAULT '[]',
    block_cidrs     TEXT NOT NULL DEFAULT '[]',
    allow_countries TEXT NOT NULL DEFAULT '[]',
    block_countries TEXT NOT NULL DEFAULT '[]'
);
-- mtls CA per service (Part J.2):
ALTER TABLE services ADD COLUMN mtls_ca_pem TEXT;

-- +goose Down
ALTER TABLE services DROP COLUMN mtls_ca_pem;   -- SQLite 3.35+; verify in test
DROP TABLE service_ip_geo;
DROP TABLE budgets;
DROP TABLE rate_limits;
DROP TABLE model_aliases;
DROP TABLE service_ai_config;
