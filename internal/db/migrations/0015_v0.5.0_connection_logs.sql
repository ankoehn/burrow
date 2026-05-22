-- internal/db/migrations/0015_v0.5.0_connection_logs.sql
-- +goose Up
CREATE TABLE connection_logs (
  id              TEXT PRIMARY KEY,
  kind            TEXT NOT NULL,
  service_id      TEXT REFERENCES services(id) ON DELETE SET NULL,
  tunnel_id       TEXT,
  user_id         TEXT REFERENCES users(id) ON DELETE SET NULL,
  client_session_id TEXT,
  source_ip       TEXT NOT NULL,
  user_agent      TEXT,
  started_at      DATETIME NOT NULL,
  ended_at        DATETIME NOT NULL,
  duration_ms     INTEGER NOT NULL,
  bytes_in        INTEGER NOT NULL DEFAULT 0,
  bytes_out       INTEGER NOT NULL DEFAULT 0,
  status          TEXT NOT NULL,
  reason          TEXT,
  created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_connlog_started ON connection_logs(started_at DESC);
CREATE INDEX idx_connlog_service_started ON connection_logs(service_id, started_at DESC);
CREATE INDEX idx_connlog_kind_started ON connection_logs(kind, started_at DESC);

CREATE TABLE connection_log_rollups (
  day              DATE NOT NULL,
  service_id       TEXT NOT NULL,
  kind             TEXT NOT NULL,
  sessions         INTEGER NOT NULL DEFAULT 0,
  bytes_in         INTEGER NOT NULL DEFAULT 0,
  bytes_out        INTEGER NOT NULL DEFAULT 0,
  avg_duration_ms  INTEGER NOT NULL DEFAULT 0,
  p95_duration_ms  INTEGER NOT NULL DEFAULT 0,
  created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (day, service_id, kind)
);

-- +goose Down
DROP TABLE connection_log_rollups;
DROP TABLE connection_logs;
