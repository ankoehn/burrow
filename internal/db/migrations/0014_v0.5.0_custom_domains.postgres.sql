-- internal/db/migrations/0014_v0.5.0_custom_domains.postgres.sql
-- +goose Up
CREATE TABLE service_custom_domains (
  id             TEXT PRIMARY KEY,
  service_id     TEXT NOT NULL REFERENCES services(id) ON DELETE CASCADE,
  hostname       TEXT NOT NULL UNIQUE,
  cert_pem       TEXT NOT NULL,
  key_pem        TEXT NOT NULL,
  cert_sha256    TEXT NOT NULL,
  not_before     TIMESTAMP WITH TIME ZONE NOT NULL,
  not_after      TIMESTAMP WITH TIME ZONE NOT NULL,
  created_at     TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  updated_at     TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_custom_domains_service ON service_custom_domains(service_id);
CREATE INDEX idx_custom_domains_not_after ON service_custom_domains(not_after);

-- +goose Down
DROP TABLE service_custom_domains;
