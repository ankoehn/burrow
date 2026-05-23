package connlog

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/db"
)

// testDB opens a temp SQLite database, runs all migrations, and returns a
// *db.DB. The database is closed when t finishes.
func testDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "connlog_test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	if err := db.Migrate(d); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	x := db.Wrap(d)
	t.Cleanup(func() { _ = x.Close() })
	return x
}

// seedUser inserts a minimal users row so FK constraints are satisfied.
func seedUser(t *testing.T, x *db.DB, id string) {
	t.Helper()
	_, err := x.DB().Exec(
		`INSERT INTO users(id,email,password_hash,role) VALUES(?,?,?,'user')`,
		id, id+"@test.invalid", "hash",
	)
	if err != nil {
		t.Fatalf("seedUser: %v", err)
	}
}

// seedService inserts a minimal services row.
func seedService(t *testing.T, x *db.DB, id, userID string) {
	t.Helper()
	_, err := x.DB().Exec(
		`INSERT INTO services(id,user_id,name,type,subdomain,access_mode,api_key_header)
		 VALUES(?,?,'svc','http','sub','open','Authorization')`,
		id, userID,
	)
	if err != nil {
		t.Fatalf("seedService: %v", err)
	}
}

// waitForRows polls until the connection_logs table has at least n rows or
// times out after 2 s. Required because Record spawns a goroutine.
func waitForRows(t *testing.T, x *db.DB, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		var count int
		_ = x.DB().QueryRow(`SELECT COUNT(*) FROM connection_logs`).Scan(&count)
		if count >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout: want %d connection_log rows, got %d", n, count)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestConnLogSinkRecordsAndQueryReturnsDescendingByStartedAt inserts three
// entries with started_at staggered by 1 s and asserts ListConnectionLogs
// returns them newest-first.
func TestConnLogSinkRecordsAndQueryReturnsDescendingByStartedAt(t *testing.T) {
	x := testDB(t)
	sink := NewSQLSink(x, nil)
	ctx := context.Background()

	base := time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		start := base.Add(time.Duration(i) * time.Second)
		end := start.Add(100 * time.Millisecond)
		if err := sink.Record(ctx, Entry{
			Kind:      KindHTTPProxy,
			SourceIP:  "10.0.0.1",
			StartedAt: start,
			EndedAt:   end,
			Status:    StatusClosedClean,
		}); err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}

	waitForRows(t, x, 3)

	rows, err := ListConnectionLogs(ctx, x, ConnLogQuery{Limit: 10})
	if err != nil {
		t.Fatalf("ListConnectionLogs: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	// Newest first.
	if !rows[0].StartedAt.After(rows[1].StartedAt) {
		t.Errorf("rows[0].StartedAt=%v should be after rows[1].StartedAt=%v",
			rows[0].StartedAt, rows[1].StartedAt)
	}
	if !rows[1].StartedAt.After(rows[2].StartedAt) {
		t.Errorf("rows[1].StartedAt=%v should be after rows[2].StartedAt=%v",
			rows[1].StartedAt, rows[2].StartedAt)
	}
}

// TestConnLogSinkFiltersOnKind asserts that the kind= filter is applied.
func TestConnLogSinkFiltersOnKind(t *testing.T) {
	x := testDB(t)
	sink := NewSQLSink(x, nil)
	ctx := context.Background()

	now := time.Now().UTC()
	for _, kind := range []Kind{KindHTTPProxy, KindTCPProxy, KindControl} {
		_ = sink.Record(ctx, Entry{
			Kind:      kind,
			SourceIP:  "10.0.0.1",
			StartedAt: now,
			EndedAt:   now.Add(50 * time.Millisecond),
			Status:    StatusClosedClean,
		})
	}
	waitForRows(t, x, 3)

	rows, err := ListConnectionLogs(ctx, x, ConnLogQuery{Kind: "http_proxy", Limit: 10})
	if err != nil {
		t.Fatalf("ListConnectionLogs: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 http_proxy row, got %d", len(rows))
	}
	if rows[0].Kind != "http_proxy" {
		t.Errorf("unexpected kind %q", rows[0].Kind)
	}
}

// TestConnLogSinkCursorPagination verifies that ?before_id= returns older rows.
func TestConnLogSinkCursorPagination(t *testing.T) {
	x := testDB(t)
	sink := NewSQLSink(x, nil)
	ctx := context.Background()

	base := time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		start := base.Add(time.Duration(i) * time.Second)
		_ = sink.Record(ctx, Entry{
			Kind:      KindHTTPProxy,
			SourceIP:  "10.0.0.1",
			StartedAt: start,
			EndedAt:   start.Add(10 * time.Millisecond),
			Status:    StatusClosedClean,
		})
	}
	waitForRows(t, x, 5)

	// First page: 2 newest.
	page1, err := ListConnectionLogs(ctx, x, ConnLogQuery{Limit: 2})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("want 2 rows on page1, got %d", len(page1))
	}
	cursorID := page1[len(page1)-1].ID

	// Second page: rows older than cursorID.
	page2, err := ListConnectionLogs(ctx, x, ConnLogQuery{BeforeID: cursorID, Limit: 10})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 3 {
		t.Fatalf("want 3 rows on page2, got %d", len(page2))
	}
	// All page2 rows must be older than the cursor.
	cursorTime := page1[len(page1)-1].StartedAt
	for _, r := range page2 {
		if !r.StartedAt.Before(cursorTime) {
			t.Errorf("page2 row %s started_at=%v not before cursor=%v", r.ID, r.StartedAt, cursorTime)
		}
	}
}

// TestRecordHonoursDetachedContext verifies that Record still lands the row in
// the DB even when the caller's context is already cancelled at call time.
// This covers the proxy hot-path pattern:
//
//	defer sink.Record(r.Context(), entry)
//
// where r.Context() is cancelled the moment the handler returns.
func TestRecordHonoursDetachedContext(t *testing.T) {
	x := testDB(t)
	sink := NewSQLSink(x, nil)

	// Pre-cancel the context before calling Record.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	now := time.Now().UTC()
	if err := sink.Record(ctx, Entry{
		Kind:      KindHTTPProxy,
		SourceIP:  "192.0.2.1",
		StartedAt: now,
		EndedAt:   now.Add(50 * time.Millisecond),
		Status:    StatusClosedClean,
	}); err != nil {
		t.Fatalf("Record returned unexpected error: %v", err)
	}

	// Poll until the row lands or we time out.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entries, _ := ListConnectionLogs(context.Background(), x, ConnLogQuery{Limit: 10})
		if len(entries) >= 1 {
			return // success
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("entry never landed in DB despite pre-cancelled caller context")
}

// TestRollupIdempotentAndComputesP95 inserts 100 entries with durations
// 100..199 ms for one service/day, runs Rollup twice, and asserts:
//   - exactly 1 rollup row exists (idempotent),
//   - p95_duration_ms == 195 (index 95 of 100 sorted values 100..199).
func TestRollupIdempotentAndComputesP95(t *testing.T) {
	x := testDB(t)
	ctx := context.Background()

	seedUser(t, x, "u1")
	seedService(t, x, "s1", "u1")

	sink := NewSQLSink(x, nil)

	day := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	base := day.Add(1 * time.Hour)

	// Insert 100 rows with durations 100..199 ms directly (synchronous Exec)
	// so we don't need to poll.
	for i := 0; i < 100; i++ {
		dur := 100 + i // ms
		start := base.Add(time.Duration(i) * time.Second)
		end := start.Add(time.Duration(dur) * time.Millisecond)
		_, err := x.DB().ExecContext(ctx,
			`INSERT INTO connection_logs
			  (id, kind, service_id, source_ip, started_at, ended_at,
			   duration_ms, bytes_in, bytes_out, status)
			 VALUES (?,?,?,?,?,?,?,?,?,?)`,
			fmt.Sprintf("cl-%03d", i), // unique ID
			"http_proxy", "s1", "1.2.3.4",
			start.UTC(), end.UTC(),
			dur, 0, 0, "closed_clean",
		)
		if err != nil {
			t.Fatalf("insert[%d]: %v", i, err)
		}
	}

	// First rollup.
	if err := sink.Rollup(ctx, day); err != nil {
		t.Fatalf("Rollup(1st): %v", err)
	}

	// Verify row count and p95.
	var rowCount int
	_ = x.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM connection_log_rollups`).Scan(&rowCount)
	if rowCount != 1 {
		t.Fatalf("want 1 rollup row, got %d", rowCount)
	}

	var p95 int64
	var sessions int64
	_ = x.DB().QueryRowContext(ctx,
		`SELECT p95_duration_ms, sessions FROM connection_log_rollups WHERE day=? AND service_id='s1' AND kind='http_proxy'`,
		day.Format("2006-01-02"),
	).Scan(&p95, &sessions)

	if sessions != 100 {
		t.Errorf("want sessions=100, got %d", sessions)
	}
	// p95 of [100..199] (n=100, sorted): index floor(100 * 0.95) = 95 → value 195.
	if p95 != 195 {
		t.Errorf("want p95=195, got %d", p95)
	}

	// Second rollup (idempotent).
	if err := sink.Rollup(ctx, day); err != nil {
		t.Fatalf("Rollup(2nd): %v", err)
	}

	// Still only 1 row.
	_ = x.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM connection_log_rollups`).Scan(&rowCount)
	if rowCount != 1 {
		t.Fatalf("after 2nd rollup: want 1 row, got %d", rowCount)
	}
}
