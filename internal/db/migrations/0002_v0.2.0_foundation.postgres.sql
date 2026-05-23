-- internal/db/migrations/0002_v0.2.0_foundation.postgres.sql
-- +goose Up
CREATE TABLE roles (
    name        TEXT PRIMARY KEY,
    description TEXT NOT NULL,
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
INSERT INTO roles(name, description) VALUES
    ('admin', 'Full administrative access to all tunnels, client tokens, users, roles, and settings.')
    ON CONFLICT (name) DO NOTHING;
INSERT INTO roles(name, description) VALUES
    ('user',  'Standard user: manage own tunnels and own client tokens.')
    ON CONFLICT (name) DO NOTHING;

ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE users ADD COLUMN last_login TIMESTAMP WITH TIME ZONE;

ALTER TABLE tunnels ADD COLUMN total_bytes_in  BIGINT NOT NULL DEFAULT 0;
ALTER TABLE tunnels ADD COLUMN total_bytes_out BIGINT NOT NULL DEFAULT 0;
ALTER TABLE tunnels ADD COLUMN last_flushed_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE tunnels ADD COLUMN access_mode TEXT NOT NULL DEFAULT 'open';

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE settings;
DROP TABLE roles;
