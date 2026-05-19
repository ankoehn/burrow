-- internal/db/migrations/0002_v0.2.0_foundation.sql
-- +goose Up
CREATE TABLE roles (
    name        TEXT PRIMARY KEY,
    description TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO roles(name, description) VALUES
    ('admin', 'Full administrative access to all tunnels, client tokens, users, roles, and settings.'),
    ('user',  'Standard user: manage own tunnels and own client tokens.');

ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE users ADD COLUMN last_login DATETIME;

ALTER TABLE tunnels ADD COLUMN total_bytes_in  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tunnels ADD COLUMN total_bytes_out INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tunnels ADD COLUMN last_flushed_at DATETIME;
ALTER TABLE tunnels ADD COLUMN access_mode TEXT NOT NULL DEFAULT 'open';

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE settings;
DROP TABLE roles;
