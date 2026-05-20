package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAutomationTokenCRUD(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)

	if err := x.CreateUser(ctx, User{ID: "u1", Email: "a@b.c", PasswordHash: "h", Role: "admin"}); err != nil {
		t.Fatal(err)
	}

	exp := time.Now().UTC().Add(time.Hour)
	tok := AutomationToken{
		ID:          "at1",
		Name:        "ci",
		Prefix:      "bua_abcd",
		UserID:      "u1",
		RoleAtMint:  "admin",
		TokenHash:   "deadbeef",
		Permissions: `["tunnels:read:any"]`,
		ExpiresAt:   &exp,
	}
	if err := x.CreateAutomationToken(ctx, tok); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := x.GetAutomationToken(ctx, "at1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "ci" || got.TokenHash != "deadbeef" || got.Permissions != `["tunnels:read:any"]` {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(exp.UTC()) {
		t.Fatalf("expires_at mismatch: %v vs %v", got.ExpiresAt, exp)
	}

	hashHit, err := x.GetAutomationTokenByHash(ctx, "deadbeef")
	if err != nil || hashHit.ID != "at1" {
		t.Fatalf("by-hash: %+v %v", hashHit, err)
	}

	if _, err := x.GetAutomationTokenByHash(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("by-hash miss: want ErrNotFound got %v", err)
	}

	all, err := x.ListAutomationTokensByUser(ctx, "u1")
	if err != nil || len(all) != 1 {
		t.Fatalf("list-by-user: %v len=%d", err, len(all))
	}

	allAdmin, err := x.ListAutomationTokens(ctx)
	if err != nil || len(allAdmin) != 1 {
		t.Fatalf("list-all: %v len=%d", err, len(allAdmin))
	}

	if err := x.TouchAutomationTokenLastUsed(ctx, "at1"); err != nil {
		t.Fatalf("touch: %v", err)
	}
	got2, _ := x.GetAutomationToken(ctx, "at1")
	if got2.LastUsed == nil {
		t.Fatalf("last_used must be populated after Touch")
	}

	if err := x.DeleteAutomationToken(ctx, "at1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := x.GetAutomationToken(ctx, "at1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: want ErrNotFound got %v", err)
	}
	if err := x.DeleteAutomationToken(ctx, "at1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("re-delete: want ErrNotFound got %v", err)
	}
}

func TestAutomationTokenUserCascade(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)

	if err := x.CreateUser(ctx, User{ID: "u1", Email: "a@b.c", PasswordHash: "h", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	tok := AutomationToken{
		ID: "at1", Name: "ci", Prefix: "bua_x", UserID: "u1",
		RoleAtMint: "admin", TokenHash: "hh1", Permissions: `[]`,
	}
	if err := x.CreateAutomationToken(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := x.DB().ExecContext(ctx, `DELETE FROM users WHERE id=?`, "u1"); err != nil {
		t.Fatal(err)
	}
	if _, err := x.GetAutomationToken(ctx, "at1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("user delete must cascade automation_tokens; got %v", err)
	}
}

func TestAutomationTokenNoExpiryAllowed(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)
	if err := x.CreateUser(ctx, User{ID: "u1", Email: "a@b.c", PasswordHash: "h", Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	tok := AutomationToken{
		ID: "at1", Name: "ci", Prefix: "bua_x", UserID: "u1",
		RoleAtMint: "admin", TokenHash: "hh1", Permissions: `[]`,
		// ExpiresAt left nil — must persist as NULL and round-trip.
	}
	if err := x.CreateAutomationToken(ctx, tok); err != nil {
		t.Fatalf("create no-expiry: %v", err)
	}
	got, err := x.GetAutomationToken(ctx, "at1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ExpiresAt != nil {
		t.Fatalf("ExpiresAt nil round-trip: got %v", got.ExpiresAt)
	}
}
