-- internal/db/migrations/0006_v0.4.0_audit.postgres.sql
-- +goose Up
CREATE TABLE audit_events (
    id            TEXT PRIMARY KEY,         -- ulid (sortable)
    ts            TIMESTAMP WITH TIME ZONE NOT NULL,
    actor_id      TEXT NOT NULL DEFAULT '',
    actor_email   TEXT NOT NULL DEFAULT '',
    action        TEXT NOT NULL,
    subject_id    TEXT NOT NULL DEFAULT '',
    subject_label TEXT NOT NULL DEFAULT '',
    result        TEXT NOT NULL,            -- ok|denied|error
    source_ip     TEXT NOT NULL DEFAULT '',
    user_agent    TEXT NOT NULL DEFAULT '',
    request_id    TEXT NOT NULL DEFAULT '',
    payload       TEXT NOT NULL DEFAULT '{}',
    prev_hash     TEXT NOT NULL,
    hash          TEXT NOT NULL UNIQUE
);
CREATE INDEX audit_events_ts       ON audit_events(ts DESC);
CREATE INDEX audit_events_action   ON audit_events(action);
CREATE INDEX audit_events_actor_id ON audit_events(actor_id);

-- +goose Down
DROP TABLE audit_events;
