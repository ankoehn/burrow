package db

import (
	"context"
	"testing"
	"time"
)

func TestSessionListAndRevoke(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)
	_ = x.CreateUser(ctx, User{ID: "u1", Email: "a@b.c", PasswordHash: "h", Role: "user"})
	_ = x.CreateUser(ctx, User{ID: "u2", Email: "x@y.z", PasswordHash: "h", Role: "user"})
	exp := time.Now().UTC().Add(time.Hour)
	_ = x.CreateSession(ctx, Session{ID: "s1", UserID: "u1", ExpiresAt: exp, IP: "1.1.1.1", UserAgent: "A"})
	_ = x.CreateSession(ctx, Session{ID: "s2", UserID: "u1", ExpiresAt: exp})
	_ = x.CreateSession(ctx, Session{ID: "s3", UserID: "u1", ExpiresAt: exp})
	_ = x.CreateSession(ctx, Session{ID: "o1", UserID: "u2", ExpiresAt: exp})

	ls, err := x.ListSessionsByUser(ctx, "u1")
	if err != nil || len(ls) != 3 {
		t.Fatalf("list u1: n=%d err=%v", len(ls), err)
	}

	// scoped single revoke: wrong owner -> ErrNotFound, no delete
	if err := x.DeleteSessionForUser(ctx, "o1", "u1"); err != ErrNotFound {
		t.Fatalf("cross-user revoke: want ErrNotFound, got %v", err)
	}
	if err := x.DeleteSessionForUser(ctx, "s1", "u1"); err != nil {
		t.Fatal(err)
	}

	// revoke all except current
	n, err := x.DeleteSessionsByUserExcept(ctx, "u1", "s2")
	if err != nil || n != 1 { // s3 removed, s2 kept (s1 already gone)
		t.Fatalf("revoke-all-except: n=%d err=%v", n, err)
	}
	ls, _ = x.ListSessionsByUser(ctx, "u1")
	if len(ls) != 1 || ls[0].ID != "s2" {
		t.Fatalf("remaining: %+v", ls)
	}

	// revoke all (used at suspend time)
	n, err = x.DeleteSessionsByUser(ctx, "u1")
	if err != nil || n != 1 {
		t.Fatalf("revoke-all: n=%d err=%v", n, err)
	}
	if ls, _ := x.ListSessionsByUser(ctx, "u1"); len(ls) != 0 {
		t.Fatalf("want 0 after revoke-all, got %d", len(ls))
	}
}
