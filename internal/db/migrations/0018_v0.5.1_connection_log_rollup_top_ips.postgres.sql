-- internal/db/migrations/0018_connection_log_rollups_top_ips.postgres.sql
-- v0.5.1 Task 5 (P2.1): top-source-IPs aux table for connection_log_rollups.
--
-- Per (day, service_id, kind, ip) tuple holds the per-day per-service-per-kind
-- session count attributed to that source IP. SQLSink.Rollup populates the top
-- 10 IPs per group when the settings.connection_logs.rollup_include_top_ips
-- toggle is true (or unset — default true).

-- +goose Up
CREATE TABLE connection_log_rollup_top_ips (
  day         DATE NOT NULL,
  service_id  TEXT NOT NULL,
  kind        TEXT NOT NULL,
  ip          TEXT NOT NULL,
  sessions    BIGINT NOT NULL,
  PRIMARY KEY (day, service_id, kind, ip)
);
CREATE INDEX idx_clr_top_ips_day ON connection_log_rollup_top_ips(day);

-- +goose Down
DROP INDEX IF EXISTS idx_clr_top_ips_day;
DROP TABLE IF EXISTS connection_log_rollup_top_ips;
