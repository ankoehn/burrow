package db

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestAuditInsertAndIter asserts the InsertAuditEvent + IterAuditEventsAsc
// round-trip preserves every column and that iteration order is id ASC.
func TestAuditInsertAndIter(t *testing.T) {
	x := testDB(t)
	ctx := context.Background()
	ts := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)

	tx, err := x.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	rows := []AuditEventInsert{
		{ID: "01", Ts: ts, Action: "user.create", Result: "ok",
			Payload: `{}`, PrevHash: strings.Repeat("0", 64), Hash: "h1"},
		{ID: "02", Ts: ts.Add(time.Second), Action: "user.update", Result: "ok",
			Payload: `{"a":1}`, PrevHash: "h1", Hash: "h2"},
	}
	for _, r := range rows {
		if err := x.InsertAuditEvent(ctx, tx, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// Latest hash = the last inserted row.
	latest, ok, err := x.LatestAuditHash(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || latest != "h2" {
		t.Fatalf("latest=%q ok=%v, want h2", latest, ok)
	}

	var seen []string
	if err := x.IterAuditEventsAsc(ctx, "", "", func(e AuditEvent) error {
		seen = append(seen, e.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != "01" || seen[1] != "02" {
		t.Fatalf("iter order: %v", seen)
	}
}

// TestAuditListFilters covers each query parameter individually.
func TestAuditListFilters(t *testing.T) {
	x := testDB(t)
	ctx := context.Background()
	base := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)

	tx, err := x.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i, r := range []AuditEventInsert{
		{ID: "01", Ts: base.Add(0 * time.Second), Action: "user.create",
			ActorEmail: "a@x", SubjectLabel: "alice",
			Result: "ok", Payload: `{}`, PrevHash: strings.Repeat("0", 64), Hash: "h1"},
		{ID: "02", Ts: base.Add(1 * time.Second), Action: "user.delete",
			ActorEmail: "a@x", SubjectLabel: "bob",
			Result: "ok", Payload: `{}`, PrevHash: "h1", Hash: "h2"},
		{ID: "03", Ts: base.Add(2 * time.Second), Action: "user.create",
			ActorEmail: "b@x", SubjectLabel: "carol",
			Result: "ok", Payload: `{}`, PrevHash: "h2", Hash: "h3"},
	} {
		if err := x.InsertAuditEvent(ctx, tx, r); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	rows, err := x.ListAuditEvents(ctx, AuditQuery{Action: "user.create", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("action filter: want 2, got %d", len(rows))
	}
	// id DESC: row 03 comes first.
	if rows[0].ID != "03" || rows[1].ID != "01" {
		t.Fatalf("expected id-DESC order [03 01], got [%s %s]", rows[0].ID, rows[1].ID)
	}

	rows, err = x.ListAuditEvents(ctx, AuditQuery{Actor: "b@x", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "03" {
		t.Fatalf("actor filter: %+v", rows)
	}

	rows, err = x.ListAuditEvents(ctx, AuditQuery{Q: "alice", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "01" {
		t.Fatalf("q-like filter (alice): %+v", rows)
	}

	rows, err = x.ListAuditEvents(ctx, AuditQuery{BeforeID: "03", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "02" {
		t.Fatalf("before_id cursor: %+v", rows)
	}

	rows, err = x.ListAuditEvents(ctx, AuditQuery{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "03" {
		t.Fatalf("limit cap: %+v", rows)
	}
}

// TestAuditTamperHelperUpdatesPayload verifies the test-only tamper helper
// changes the payload column WITHOUT touching the hash.
func TestAuditTamperHelperUpdatesPayload(t *testing.T) {
	x := testDB(t)
	ctx := context.Background()
	tx, err := x.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := x.InsertAuditEvent(ctx, tx, AuditEventInsert{
		ID: "01", Ts: time.Now().UTC(), Action: "user.create", Result: "ok",
		Payload: `{}`, PrevHash: strings.Repeat("0", 64), Hash: "h-original",
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := x.TamperAuditPayload(ctx, "01", `{"role":"admin"}`); err != nil {
		t.Fatal(err)
	}
	rows, _ := x.ListAuditEvents(ctx, AuditQuery{Limit: 10})
	if rows[0].Payload != `{"role":"admin"}` || rows[0].Hash != "h-original" {
		t.Fatalf("tamper didn't behave: payload=%s hash=%s", rows[0].Payload, rows[0].Hash)
	}
}
