-- internal/db/migrations/0007_v0.4.0_webauthn.sql
-- +goose Up
CREATE TABLE webauthn_credentials (
    id          TEXT PRIMARY KEY,           -- base64url credential id
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    label       TEXT NOT NULL,
    public_key  BLOB NOT NULL,
    sign_count  INTEGER NOT NULL DEFAULT 0,
    aaguid      TEXT,
    transports  TEXT,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used   DATETIME
);
CREATE INDEX webauthn_credentials_user_id ON webauthn_credentials(user_id);

-- +goose Down
DROP TABLE webauthn_credentials;
