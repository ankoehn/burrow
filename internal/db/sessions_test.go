package db

import (
	"context"
	"testing"
	"time"
)

// TestDeleteExpiredSessions_PastAndFuture verifies that DeleteExpiredSessions
// removes only expired rows and leaves future rows intact.
func TestDeleteExpiredSessions_PastAndFuture(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)

	_ = x.CreateUser(ctx, User{ID: "u1", Email: "expire@test", PasswordHash: "h", Role: "admin"})

	// One row already expired, one not yet expired.
	_ = x.CreateSession(ctx, Session{ID: "past", UserID: "u1", ExpiresAt: time.Now().UTC().Add(-time.Hour)})
	_ = x.CreateSession(ctx, Session{ID: "future", UserID: "u1", ExpiresAt: time.Now().UTC().Add(time.Hour)})

	n, err := x.DeleteExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 row deleted, got %d", n)
	}

	// Past row must be gone.
	if _, err := x.GetSession(ctx, "past"); err != ErrNotFound {
		t.Fatalf("expired session should be gone, got %v", err)
	}
	// Future row must survive.
	if s, err := x.GetSession(ctx, "future"); err != nil || s.UserID != "u1" {
		t.Fatalf("live session should still exist: %+v %v", s, err)
	}
}

// TestDeleteExpiredSessions_OffsetRobust tests that a session created via the
// normal store code path (time.Time binding through modernc/sqlite) is purged
// correctly. This is the offset-robustness test: it round-trips a real
// store-path ExpiresAt and confirms it is comparable to the formatted NOW used
// by DeleteExpiredSessions, regardless of modernc/sqlite's serialisation format.
func TestDeleteExpiredSessions_OffsetRobust(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)

	_ = x.CreateUser(ctx, User{ID: "u2", Email: "robust@test", PasswordHash: "h", Role: "admin"})

	// Store via the same path as store.CreateSession: bind time.Time directly.
	// modernc/sqlite v1.50.1 serialises this as RFC3339Nano (e.g. "2026-05-25T12:34:56.123Z").
	expiredAt := time.Now().UTC().Add(-2 * time.Minute)
	_ = x.CreateSession(ctx, Session{ID: "robust-expired", UserID: "u2", ExpiresAt: expiredAt})

	n, err := x.DeleteExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 row purged, got %d (offset-format mismatch?)", n)
	}
	if _, err := x.GetSession(ctx, "robust-expired"); err != ErrNotFound {
		t.Fatalf("store-path expired session should be purged, got %v", err)
	}
}

// TestDeleteExpiredSessions_Empty ensures the function is a no-op (returns 0, nil)
// when there are no expired sessions.
func TestDeleteExpiredSessions_Empty(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)

	_ = x.CreateUser(ctx, User{ID: "u3", Email: "empty@test", PasswordHash: "h", Role: "admin"})
	_ = x.CreateSession(ctx, Session{ID: "only-live", UserID: "u3", ExpiresAt: time.Now().UTC().Add(24 * time.Hour)})

	n, err := x.DeleteExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}
	if n != 0 {
		t.Fatalf("want 0 deleted, got %d", n)
	}
}
