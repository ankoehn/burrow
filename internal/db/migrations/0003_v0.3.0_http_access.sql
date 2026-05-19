-- internal/db/migrations/0003_v0.3.0_http_access.sql
-- +goose Up
CREATE TABLE services (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    type           TEXT NOT NULL DEFAULT 'tcp',
    subdomain      TEXT UNIQUE,
    access_mode    TEXT NOT NULL DEFAULT 'open',
    api_key_header TEXT NOT NULL DEFAULT 'Authorization',
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, name)
);

CREATE TABLE service_api_keys (
    id         TEXT PRIMARY KEY,
    service_id TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    key_hash   TEXT NOT NULL UNIQUE,
    last_used  DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE service_access_policy (
    service_id TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    role       TEXT NOT NULL,
    PRIMARY KEY (service_id, role)
);

CREATE INDEX idx_service_api_keys_service ON service_api_keys(service_id);

-- +goose Down
DROP INDEX idx_service_api_keys_service;
DROP TABLE service_access_policy;
DROP TABLE service_api_keys;
DROP TABLE services;
