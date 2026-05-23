-- internal/db/migrations/0019_v0.5.2_custom_domain_status.postgres.sql
-- v0.5.2 Task 10: status state machine for service_custom_domains.
--
-- Adds `status` (closed enum: pending|active|cert_expiring|cert_expired) and
-- `status_updated_at` for the daily-tick state machine in
-- internal/proxy/customdomain. CHECK constraint enforces the enum at the DB
-- layer; the SQLite twin enforces it application-side via
-- internal/proxy/customdomain.ComputeStatus.

-- +goose Up
ALTER TABLE service_custom_domains ADD COLUMN status TEXT NOT NULL DEFAULT 'active'
  CHECK (status IN ('pending','active','cert_expiring','cert_expired'));
ALTER TABLE service_custom_domains ADD COLUMN status_updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now();
CREATE INDEX idx_scd_status ON service_custom_domains(status);

-- +goose Down
DROP INDEX IF EXISTS idx_scd_status;
ALTER TABLE service_custom_domains DROP COLUMN status_updated_at;
ALTER TABLE service_custom_domains DROP COLUMN status;
