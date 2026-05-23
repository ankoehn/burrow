-- internal/db/migrations/0016_v0.5.0_webhook_templates.postgres.sql
-- +goose Up
ALTER TABLE webhooks ADD COLUMN payload_template TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE webhooks DROP COLUMN IF EXISTS payload_template;
