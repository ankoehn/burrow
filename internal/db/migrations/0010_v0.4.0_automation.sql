-- internal/db/migrations/0010_v0.4.0_automation.sql
-- +goose Up
CREATE TABLE automation_tokens (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    prefix        TEXT NOT NULL,
    user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_at_mint  TEXT NOT NULL,
    token_hash    TEXT NOT NULL UNIQUE,
    permissions   TEXT NOT NULL DEFAULT '[]',
    expires_at    DATETIME,
    last_used     DATETIME,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX automation_tokens_user_id ON automation_tokens(user_id);

-- +goose Down
DROP TABLE automation_tokens;
