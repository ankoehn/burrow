-- internal/db/migrations/0004_v0.4.0_ai_meter.sql
-- +goose Up
CREATE TABLE usage_events (
    id              TEXT PRIMARY KEY,
    service_id      TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    api_key_id      TEXT NOT NULL DEFAULT '',
    ts              DATETIME NOT NULL,
    kind            TEXT NOT NULL,                 -- openai|anthropic|mcp|unknown
    tokens_in       INTEGER NOT NULL DEFAULT 0,
    tokens_out      INTEGER NOT NULL DEFAULT 0,
    bytes_in        INTEGER NOT NULL DEFAULT 0,
    bytes_out       INTEGER NOT NULL DEFAULT 0,
    streamed        INTEGER NOT NULL DEFAULT 0,
    cache_hit       INTEGER NOT NULL DEFAULT 0,
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
    body         BLOB NOT NULL,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ttl_seconds  INTEGER NOT NULL,
    last_hit_at  DATETIME
);
CREATE INDEX cache_entries_scope ON cache_entries(scope_key);

-- +goose Down
DROP TABLE cache_entries;
DROP TABLE usage_events;
