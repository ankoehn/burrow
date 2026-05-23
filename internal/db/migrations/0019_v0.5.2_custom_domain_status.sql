-- internal/db/migrations/0019_v0.5.2_custom_domain_status.sql
-- v0.5.2 Task 10: status state machine for service_custom_domains.
--
-- Adds `status` (closed enum: pending|active|cert_expiring|cert_expired) and
-- `status_updated_at` for the daily-tick state machine in
-- internal/proxy/customdomain. Enum is enforced application-side on SQLite via
-- internal/proxy/customdomain.ComputeStatus; the Postgres twin adds a CHECK
-- constraint as defense in depth.

-- +goose Up
ALTER TABLE service_custom_domains ADD COLUMN status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE service_custom_domains ADD COLUMN status_updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP;
CREATE INDEX idx_scd_status ON service_custom_domains(status);

-- +goose Down
DROP INDEX IF EXISTS idx_scd_status;
ALTER TABLE service_custom_domains DROP COLUMN status_updated_at;
ALTER TABLE service_custom_domains DROP COLUMN status;
