-- internal/db/migrations/0012_v0.5.0_upstream_credentials.sql
-- +goose Up
CREATE TABLE service_upstream_credentials (
  service_id     TEXT PRIMARY KEY REFERENCES services(id) ON DELETE CASCADE,
  slot           TEXT NOT NULL,
  header_name    TEXT NOT NULL DEFAULT 'Authorization',
  header_format  TEXT NOT NULL DEFAULT 'Bearer {key}',
  created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE service_upstream_credentials;
