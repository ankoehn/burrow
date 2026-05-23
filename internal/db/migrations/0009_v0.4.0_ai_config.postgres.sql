-- internal/db/migrations/0009_v0.4.0_ai_config.postgres.sql
-- +goose Up
CREATE TABLE service_ai_config (
    service_id  TEXT PRIMARY KEY REFERENCES services(id) ON DELETE CASCADE,
    config      JSONB NOT NULL DEFAULT '{}', -- ServiceAIConfig JSON (spec Part B.7)
    updated_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE TABLE model_aliases (
    alias          TEXT PRIMARY KEY,
    concrete_model TEXT NOT NULL,
    service_id     TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    created_at     TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE TABLE rate_limits (
    id          TEXT PRIMARY KEY,
    scope       TEXT NOT NULL,                -- api_key|role|service|global
    subject     TEXT NOT NULL DEFAULT '',
    dimension   TEXT NOT NULL,                -- rpm|bpm
    lim         BIGINT NOT NULL,
    burst       BIGINT NOT NULL,
    "window"    TEXT NOT NULL DEFAULT 'minute', -- minute|day
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE INDEX rate_limits_scope_subject ON rate_limits(scope, subject);
CREATE TABLE budgets (
    id                TEXT PRIMARY KEY,
    scope             TEXT NOT NULL,         -- api_key|service|user|global
    subject_id        TEXT NOT NULL DEFAULT '',
    daily_usd         DOUBLE PRECISION NOT NULL,
    action_on_exceed  TEXT NOT NULL,         -- alert_webhook|throttle_zero|disable_key
    alert_webhook_id  TEXT,
    created_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE TABLE service_ip_geo (
    service_id      TEXT PRIMARY KEY REFERENCES services(id) ON DELETE CASCADE,
    enabled         BOOLEAN NOT NULL DEFAULT false,
    allow_cidrs     JSONB NOT NULL DEFAULT '[]',
    block_cidrs     JSONB NOT NULL DEFAULT '[]',
    allow_countries JSONB NOT NULL DEFAULT '[]',
    block_countries JSONB NOT NULL DEFAULT '[]'
);
-- mtls CA per service (Part J.2):
ALTER TABLE services ADD COLUMN mtls_ca_pem TEXT;

-- +goose Down
ALTER TABLE services DROP COLUMN IF EXISTS mtls_ca_pem;
DROP TABLE service_ip_geo;
DROP TABLE budgets;
DROP TABLE rate_limits;
DROP TABLE model_aliases;
DROP TABLE service_ai_config;
