-- internal/db/migrations/0005_v0.4.0_custom_roles.sql
-- +goose Up
ALTER TABLE roles ADD COLUMN builtin INTEGER NOT NULL DEFAULT 0;
ALTER TABLE roles ADD COLUMN permissions TEXT NOT NULL DEFAULT '[]'; -- JSON array of permission keys
ALTER TABLE roles ADD COLUMN default_for_new_users INTEGER NOT NULL DEFAULT 0;
UPDATE roles SET builtin=1 WHERE name IN ('admin','user');

-- +goose Down
-- (SQLite has no DROP COLUMN before 3.35; the Down path recreates a temporary table.)
CREATE TABLE roles_tmp AS SELECT name, description, created_at FROM roles;
DROP TABLE roles;
ALTER TABLE roles_tmp RENAME TO roles;
