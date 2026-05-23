-- internal/db/migrations/0015_v0.5.0_connection_logs.postgres.sql
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
  started_at      TIMESTAMP WITH TIME ZONE NOT NULL,
  ended_at        TIMESTAMP WITH TIME ZONE NOT NULL,
  duration_ms     BIGINT NOT NULL,
  bytes_in        BIGINT NOT NULL DEFAULT 0,
  bytes_out       BIGINT NOT NULL DEFAULT 0,
  status          TEXT NOT NULL,
  reason          TEXT,
  created_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_connlog_started ON connection_logs(started_at DESC);
CREATE INDEX idx_connlog_service_started ON connection_logs(service_id, started_at DESC);
CREATE INDEX idx_connlog_kind_started ON connection_logs(kind, started_at DESC);

CREATE TABLE connection_log_rollups (
  day              DATE NOT NULL,
  service_id       TEXT NOT NULL,
  kind             TEXT NOT NULL,
  sessions         BIGINT NOT NULL DEFAULT 0,
  bytes_in         BIGINT NOT NULL DEFAULT 0,
  bytes_out        BIGINT NOT NULL DEFAULT 0,
  avg_duration_ms  BIGINT NOT NULL DEFAULT 0,
  p95_duration_ms  BIGINT NOT NULL DEFAULT 0,
  created_at       TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  PRIMARY KEY (day, service_id, kind)
);

-- +goose Down
DROP TABLE connection_log_rollups;
DROP TABLE connection_logs;
