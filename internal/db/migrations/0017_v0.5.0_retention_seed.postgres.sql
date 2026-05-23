-- internal/db/migrations/0017_v0.5.0_retention_seed.postgres.sql
-- +goose Up
INSERT INTO settings(key, value, updated_at) VALUES
  ('audit.retention_days',                      '0',   NOW())
  ON CONFLICT (key) DO NOTHING;
INSERT INTO settings(key, value, updated_at) VALUES
  ('inspector.retention_count',                 '100', NOW())
  ON CONFLICT (key) DO NOTHING;
INSERT INTO settings(key, value, updated_at) VALUES
  ('usage.retention_days',                      '90',  NOW())
  ON CONFLICT (key) DO NOTHING;
INSERT INTO settings(key, value, updated_at) VALUES
  ('redaction.retention_days',                  '30',  NOW())
  ON CONFLICT (key) DO NOTHING;
INSERT INTO settings(key, value, updated_at) VALUES
  ('connection_logs.retention_days',            '30',  NOW())
  ON CONFLICT (key) DO NOTHING;
INSERT INTO settings(key, value, updated_at) VALUES
  ('connection_logs.rollups_retention_days',    '0',   NOW())
  ON CONFLICT (key) DO NOTHING;
INSERT INTO settings(key, value, updated_at) VALUES
  ('webhook_deliveries.retention_days',         '30',  NOW())
  ON CONFLICT (key) DO NOTHING;

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
