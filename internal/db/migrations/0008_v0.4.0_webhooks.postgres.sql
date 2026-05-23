-- internal/db/migrations/0008_v0.4.0_webhooks.postgres.sql
-- +goose Up
CREATE TABLE webhooks (
    id                   TEXT PRIMARY KEY,
    name                 TEXT NOT NULL,
    url                  TEXT NOT NULL,
    secret_hash          TEXT NOT NULL,            -- sha256 of plaintext (shown once at create)
    events               JSONB NOT NULL,            -- JSON array of event keys
    paused               BOOLEAN NOT NULL DEFAULT false,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    first_failure_at     TIMESTAMP WITH TIME ZONE,
    created_at           TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE TABLE webhook_deliveries (
    id               TEXT PRIMARY KEY,
    webhook_id       TEXT NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event            TEXT NOT NULL,
    ts               TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    status_code      INTEGER NOT NULL DEFAULT 0,
    attempt          INTEGER NOT NULL,
    latency_ms       BIGINT NOT NULL DEFAULT 0,
    request_preview  TEXT,
    response_preview TEXT
);
CREATE INDEX webhook_deliveries_webhook_ts ON webhook_deliveries(webhook_id, ts DESC);

-- +goose Down
DROP TABLE webhook_deliveries;
DROP TABLE webhooks;
