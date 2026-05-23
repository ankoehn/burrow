-- internal/db/migrations/0013_v0.5.0_multi_provider.postgres.sql
-- +goose Up
ALTER TABLE model_aliases ADD COLUMN provider TEXT NOT NULL DEFAULT '';
ALTER TABLE model_aliases ADD COLUMN priority INTEGER NOT NULL DEFAULT 100;
CREATE INDEX idx_model_aliases_alias_priority
  ON model_aliases(alias, priority ASC);

-- +goose Down
DROP INDEX idx_model_aliases_alias_priority;
ALTER TABLE model_aliases DROP COLUMN IF EXISTS priority;
ALTER TABLE model_aliases DROP COLUMN IF EXISTS provider;
