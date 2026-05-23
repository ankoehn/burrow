-- internal/db/migrations/0005_v0.4.0_custom_roles.postgres.sql
-- +goose Up
ALTER TABLE roles ADD COLUMN builtin BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE roles ADD COLUMN permissions JSONB NOT NULL DEFAULT '[]'; -- JSON array of permission keys
ALTER TABLE roles ADD COLUMN default_for_new_users BOOLEAN NOT NULL DEFAULT false;
UPDATE roles SET builtin=true WHERE name IN ('admin','user');

-- +goose Down
ALTER TABLE roles DROP COLUMN IF EXISTS default_for_new_users;
ALTER TABLE roles DROP COLUMN IF EXISTS permissions;
ALTER TABLE roles DROP COLUMN IF EXISTS builtin;
