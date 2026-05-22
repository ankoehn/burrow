-- internal/db/migrations/0016_v0.5.0_webhook_templates.sql
-- +goose Up
ALTER TABLE webhooks ADD COLUMN payload_template TEXT NOT NULL DEFAULT '';

-- +goose Down
-- (SQLite DROP COLUMN limitation — leave column on Down.)
