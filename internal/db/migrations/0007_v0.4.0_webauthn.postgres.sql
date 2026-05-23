-- internal/db/migrations/0007_v0.4.0_webauthn.postgres.sql
-- +goose Up
CREATE TABLE webauthn_credentials (
    id          TEXT PRIMARY KEY,           -- base64url credential id
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    label       TEXT NOT NULL,
    public_key  BYTEA NOT NULL,
    sign_count  BIGINT NOT NULL DEFAULT 0,
    aaguid      TEXT,
    transports  TEXT,
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    last_used   TIMESTAMP WITH TIME ZONE
);
CREATE INDEX webauthn_credentials_user_id ON webauthn_credentials(user_id);

-- +goose Down
DROP TABLE webauthn_credentials;
