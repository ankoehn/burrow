-- internal/db/migrations/0013_v0.5.0_multi_provider.sql
-- +goose Up
ALTER TABLE model_aliases ADD COLUMN provider TEXT NOT NULL DEFAULT '';
ALTER TABLE model_aliases ADD COLUMN priority INTEGER NOT NULL DEFAULT 100;
CREATE INDEX idx_model_aliases_alias_priority
  ON model_aliases(alias, priority ASC);

-- +goose Down
DROP INDEX idx_model_aliases_alias_priority;
-- (SQLite doesn't support DROP COLUMN cleanly before 3.35; we leave the columns
--  on Down — Burrow's migrate runner only applies Up in v0.5.0.)
