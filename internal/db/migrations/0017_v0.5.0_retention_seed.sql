-- internal/db/migrations/0017_v0.5.0_retention_seed.sql
-- +goose Up
INSERT OR IGNORE INTO settings(key, value, updated_at) VALUES
  ('audit.retention_days',                      '0',  CURRENT_TIMESTAMP),
  ('inspector.retention_count',                 '100', CURRENT_TIMESTAMP),
  ('usage.retention_days',                      '90',  CURRENT_TIMESTAMP),
  ('redaction.retention_days',                  '30',  CURRENT_TIMESTAMP),
  ('connection_logs.retention_days',            '30',  CURRENT_TIMESTAMP),
  ('connection_logs.rollups_retention_days',    '0',   CURRENT_TIMESTAMP),
  ('webhook_deliveries.retention_days',         '30',  CURRENT_TIMESTAMP);

-- +goose Down
DELETE FROM settings WHERE key IN (
  'audit.retention_days',
  'inspector.retention_count',
  'usage.retention_days',
  'redaction.retention_days',
  'connection_logs.retention_days',
  'connection_logs.rollups_retention_days',
  'webhook_deliveries.retention_days'
);
