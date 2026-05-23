package main

// e2e_v050_connlog_privacy_test.go — Integration Task 9 (v0.5.0):
// Connection-logs Q12 privacy toggle. Was DEFERRED to v0.5.1; activated under
// v0.5.1 Task 5 (P2.1).
//
// The v0.5.0 API contract spec (Part E + Q12) documents a
// `connection_logs.rollup_include_top_ips` setting (default true) that
// controls whether the per-day rollup carries a `top_source_ips` field. The
// underlying machinery shipped under v0.5.1 Task 5:
//
//  1. Migration 0018 adds the connection_log_rollup_top_ips aux table.
//  2. SQLSink.Rollup aggregates the top 10 source IPs per (day, service_id,
//     kind) group when the setting is "true" or unset; deletes any
//     pre-existing aux rows when the setting flips to "false".
//  3. internal/api/connection_log_handlers.go::GetConnectionLogRollups joins
//     the aux table and emits top_source_ips conditionally.
//  4. The PUT /api/v1/settings allowlist accepts the new key.
//
// This test pins the executable contract end-to-end at the SQLSink+DB level:
// plant rows, run Rollup with the default-on toggle, assert top-IP rows
// present; flip the setting to "false", re-run, assert top-IP rows gone.

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/connlog"
	"github.com/ankoehn/burrow/internal/db"
)

// settingsTableReader is a thin SettingsReader backed by the live `settings`
// table. Production wiring uses *store.Store; this in-test variant skips the
// audit layer to keep the test focused on the toggle's compaction-time
// effect.
type settingsTableReader struct{ db *sql.DB }

func (r *settingsTableReader) GetSettings(ctx context.Context) (map[string]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

// TestConnLogPrivacyTopIPsModes verifies the v0.5.1 P2.1 Q12 toggle on the
// per-day rollup compaction path.
//
//  1. Default (no settings row) → toggle is ON → after Rollup, the
//     connection_log_rollup_top_ips table holds rows for each
//     (day, service_id, kind) group.
//  2. Explicit "true" → same outcome.
//  3. Setting flipped to "false" → next Rollup deletes any pre-existing aux
//     rows for the rolled-up groups AND skips the aggregation step.
//  4. Re-flipped to "true" → aux rows re-emerge.
func TestConnLogPrivacyTopIPsModes(t *testing.T) {
	sqldb, err := db.Open(filepath.Join(t.TempDir(), "privacy.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := db.Migrate(sqldb); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	t.Cleanup(func() { _ = sqldb.Close() })
	wrapped := db.Wrap(sqldb)
	ctx := context.Background()

	// Seed user + service so the FK on connection_logs.service_id resolves.
	if _, err := sqldb.ExecContext(ctx,
		`INSERT INTO users(id,email,password_hash,role) VALUES('u1','q12@test.invalid','hash','user')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := sqldb.ExecContext(ctx,
		`INSERT INTO services(id,user_id,name,type,subdomain,access_mode,api_key_header)
		 VALUES('svc_q12','u1','web','http','q12sub','open','Authorization')`); err != nil {
		t.Fatalf("seed service: %v", err)
	}

	settings := &settingsTableReader{db: sqldb}
	sink := connlog.NewSQLSink(wrapped, nil).WithSettings(settings)

	day := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	base := day.Add(2 * time.Hour)

	// Plant 30 connection_logs rows for (day, svc_q12, http_proxy), split
	// across 5 source IPs with varied session counts so we can verify
	// ordering and counting.
	ipPlan := []struct {
		ip       string
		sessions int
	}{
		{"203.0.113.1", 12},
		{"203.0.113.2", 8},
		{"203.0.113.3", 5},
		{"203.0.113.4", 3},
		{"203.0.113.5", 2},
	}
	id := 0
	for _, p := range ipPlan {
		for s := 0; s < p.sessions; s++ {
			start := base.Add(time.Duration(id) * time.Millisecond)
			end := start.Add(50 * time.Millisecond)
			if _, err := sqldb.ExecContext(ctx,
				`INSERT INTO connection_logs
				  (id, kind, service_id, source_ip, started_at, ended_at,
				   duration_ms, bytes_in, bytes_out, status)
				 VALUES (?,?,?,?,?,?,?,0,0,'closed_clean')`,
				fmt.Sprintf("q12-%03d", id), "http_proxy", "svc_q12", p.ip,
				start.UTC(), end.UTC(), 50,
			); err != nil {
				t.Fatalf("insert q12-%d: %v", id, err)
			}
			id++
		}
	}

	dayStr := day.Format("2006-01-02")
	countTopIPs := func(t *testing.T) int {
		t.Helper()
		var n int
		if err := sqldb.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM connection_log_rollup_top_ips
			  WHERE day=? AND service_id='svc_q12' AND kind='http_proxy'`,
			dayStr,
		).Scan(&n); err != nil {
			t.Fatalf("count top_ips: %v", err)
		}
		return n
	}

	// --- 1. Default (settings row absent) → toggle ON --------------------
	if err := sink.Rollup(ctx, day); err != nil {
		t.Fatalf("Rollup(default): %v", err)
	}
	if got := countTopIPs(t); got != len(ipPlan) {
		t.Fatalf("default ON: want %d top_ips rows, got %d", len(ipPlan), got)
	}

	// Sanity: the top row by sessions should be 203.0.113.1 (12 sessions).
	var topIP string
	var topSessions int64
	if err := sqldb.QueryRowContext(ctx,
		`SELECT ip, sessions FROM connection_log_rollup_top_ips
		  WHERE day=? AND service_id='svc_q12' AND kind='http_proxy'
		  ORDER BY sessions DESC LIMIT 1`,
		dayStr,
	).Scan(&topIP, &topSessions); err != nil {
		t.Fatalf("top row query: %v", err)
	}
	if topIP != "203.0.113.1" || topSessions != 12 {
		t.Errorf("top row: want 203.0.113.1/12, got %s/%d", topIP, topSessions)
	}

	// --- 2. Explicit "true" → toggle ON (idempotent re-roll) -------------
	if _, err := sqldb.ExecContext(ctx,
		`INSERT INTO settings(key, value, updated_at)
		 VALUES('connection_logs.rollup_include_top_ips', 'true', CURRENT_TIMESTAMP)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
	); err != nil {
		t.Fatalf("set toggle true: %v", err)
	}
	if err := sink.Rollup(ctx, day); err != nil {
		t.Fatalf("Rollup(true): %v", err)
	}
	if got := countTopIPs(t); got != len(ipPlan) {
		t.Fatalf("explicit true: want %d top_ips rows, got %d", len(ipPlan), got)
	}

	// --- 3. Flip to "false" → toggle OFF → aux rows scrubbed -------------
	if _, err := sqldb.ExecContext(ctx,
		`UPDATE settings SET value='false', updated_at=CURRENT_TIMESTAMP
		  WHERE key='connection_logs.rollup_include_top_ips'`,
	); err != nil {
		t.Fatalf("set toggle false: %v", err)
	}
	if err := sink.Rollup(ctx, day); err != nil {
		t.Fatalf("Rollup(false): %v", err)
	}
	if got := countTopIPs(t); got != 0 {
		t.Fatalf("toggle OFF: want 0 top_ips rows (scrubbed), got %d", got)
	}

	// The base rollup row must remain — only the aux table is affected.
	var baseSessions int64
	if err := sqldb.QueryRowContext(ctx,
		`SELECT sessions FROM connection_log_rollups
		  WHERE day=? AND service_id='svc_q12' AND kind='http_proxy'`,
		dayStr,
	).Scan(&baseSessions); err != nil {
		t.Fatalf("base rollup row missing after toggle OFF: %v", err)
	}
	if baseSessions != int64(id) { // id == total inserted rows
		t.Errorf("base rollup sessions: want %d, got %d", id, baseSessions)
	}

	// --- 4. Flip back to "true" → aux rows re-emerge ---------------------
	if _, err := sqldb.ExecContext(ctx,
		`UPDATE settings SET value='true', updated_at=CURRENT_TIMESTAMP
		  WHERE key='connection_logs.rollup_include_top_ips'`,
	); err != nil {
		t.Fatalf("set toggle true (re-enable): %v", err)
	}
	if err := sink.Rollup(ctx, day); err != nil {
		t.Fatalf("Rollup(true re-enable): %v", err)
	}
	if got := countTopIPs(t); got != len(ipPlan) {
		t.Fatalf("re-enabled true: want %d top_ips rows, got %d", len(ipPlan), got)
	}
}
