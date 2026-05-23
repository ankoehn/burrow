-- internal/db/migrations/0004_v0.4.0_ai_meter.postgres.sql
-- +goose Up
CREATE TABLE usage_events (
    id              TEXT PRIMARY KEY,
    service_id      TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    api_key_id      TEXT NOT NULL DEFAULT '',
    ts              TIMESTAMP WITH TIME ZONE NOT NULL,
    kind            TEXT NOT NULL,                 -- openai|anthropic|mcp|unknown
    tokens_in       BIGINT NOT NULL DEFAULT 0,
    tokens_out      BIGINT NOT NULL DEFAULT 0,
    bytes_in        BIGINT NOT NULL DEFAULT 0,
    bytes_out       BIGINT NOT NULL DEFAULT 0,
    streamed        BOOLEAN NOT NULL DEFAULT false,
    cache_hit       BOOLEAN NOT NULL DEFAULT false,
    upstream_status INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX usage_events_service_ts ON usage_events(service_id, ts DESC);
CREATE INDEX usage_events_api_key_ts ON usage_events(api_key_id, ts DESC);

CREATE TABLE cache_entries (
    id           TEXT PRIMARY KEY,
    scope_key    TEXT NOT NULL,
    key_hash     TEXT NOT NULL UNIQUE,
    status       INTEGER NOT NULL,
    headers      TEXT NOT NULL,
    body         BYTEA NOT NULL,
    created_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    ttl_seconds  INTEGER NOT NULL,
    last_hit_at  TIMESTAMP WITH TIME ZONE
);
CREATE INDEX cache_entries_scope ON cache_entries(scope_key);

-- +goose Down
DROP TABLE cache_entries;
DROP TABLE usage_events;
