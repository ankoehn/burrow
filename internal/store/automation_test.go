package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/db"
)

func TestMintAutomationTokenAdminSubsetOK(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	admin := seedUser(t, s, "admin@x", "admin")

	view, plaintext, err := s.MintAutomationToken(ctx, admin.ID, "admin", "ci", []string{"tunnels:read:any"}, nil)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if !strings.HasPrefix(plaintext, "bua_") {
		t.Fatalf("plaintext prefix: %q", plaintext)
	}
	if len(view.Permissions) != 1 || view.Permissions[0] != "tunnels:read:any" {
		t.Fatalf("perms round-trip: %+v", view.Permissions)
	}
	if view.RoleAtMint != "admin" {
		t.Fatalf("role_at_mint: %q", view.RoleAtMint)
	}
	if !strings.HasPrefix(view.Prefix, "bua_") {
		t.Fatalf("prefix col missing bua_: %q", view.Prefix)
	}

	// Lookup-by-hash returns the same row.
	sum := sha256.Sum256([]byte(plaintext))
	hash := hex.EncodeToString(sum[:])
	got, err := s.LookupBearer(ctx, hash)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.ID != view.ID {
		t.Fatalf("lookup id mismatch: %q vs %q", got.ID, view.ID)
	}
}

func TestMintAutomationTokenPermissionNotInRole(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	u := seedUser(t, s, "user@x", "user")

	// A "user" cannot escalate to users:manage.
	_, _, err := s.MintAutomationToken(ctx, u.ID, "user", "ci", []string{"users:manage"}, nil)
	if !errors.Is(err, ErrPermissionNotInRole) {
		t.Fatalf("want ErrPermissionNotInRole, got %v", err)
	}
}

func TestMintAutomationTokenEmptyName(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	admin := seedUser(t, s, "admin@x", "admin")
	_, _, err := s.MintAutomationToken(ctx, admin.ID, "admin", "  ", []string{"tunnels:read:any"}, nil)
	if !errors.Is(err, ErrTokenNameRequired) {
		t.Fatalf("want ErrTokenNameRequired, got %v", err)
	}
}

func TestListAutomationTokensScopedToOwn(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	admin := seedUser(t, s, "admin@x", "admin")
	user := seedUser(t, s, "user@x", "user")

	if _, _, err := s.MintAutomationToken(ctx, admin.ID, "admin", "a", []string{"tunnels:read:any"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.MintAutomationToken(ctx, user.ID, "user", "u", []string{"tunnels:read:own"}, nil); err != nil {
		t.Fatal(err)
	}

	// Admin sees both.
	all, err := s.ListAutomationTokensForCaller(ctx, admin.ID, "admin")
	if err != nil || len(all) != 2 {
		t.Fatalf("admin list len=%d err=%v", len(all), err)
	}

	// User sees only own.
	own, err := s.ListAutomationTokensForCaller(ctx, user.ID, "user")
	if err != nil {
		t.Fatal(err)
	}
	if len(own) != 1 || own[0].UserID != user.ID {
		t.Fatalf("user list mismatch: %+v", own)
	}
}

func TestRevokeAutomationTokenOwnerScope(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	admin := seedUser(t, s, "admin@x", "admin")
	user := seedUser(t, s, "user@x", "user")

	adminTok, _, _ := s.MintAutomationToken(ctx, admin.ID, "admin", "a", []string{"tunnels:read:any"}, nil)
	userTok, _, _ := s.MintAutomationToken(ctx, user.ID, "user", "u", []string{"tunnels:read:own"}, nil)

	// User cannot revoke admin's token — surfaces as NotFound.
	if err := s.RevokeAutomationToken(ctx, user.ID, "user", adminTok.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("foreign revoke: want db.ErrNotFound got %v", err)
	}

	// User can revoke their own.
	if err := s.RevokeAutomationToken(ctx, user.ID, "user", userTok.ID); err != nil {
		t.Fatalf("self revoke: %v", err)
	}

	// Admin can revoke anyone — re-create a user-owned token and revoke as admin.
	utok2, _, _ := s.MintAutomationToken(ctx, user.ID, "user", "u2", []string{"tunnels:read:own"}, nil)
	if err := s.RevokeAutomationToken(ctx, admin.ID, "admin", utok2.ID); err != nil {
		t.Fatalf("admin foreign revoke: %v", err)
	}
}

func TestTouchBearerUpdatesLastUsed(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	admin := seedUser(t, s, "admin@x", "admin")
	view, plaintext, _ := s.MintAutomationToken(ctx, admin.ID, "admin", "a", []string{"tunnels:read:any"}, nil)

	if err := s.TouchBearer(ctx, view.ID); err != nil {
		t.Fatalf("touch: %v", err)
	}

	sum := sha256.Sum256([]byte(plaintext))
	hash := hex.EncodeToString(sum[:])
	got, err := s.LookupBearer(ctx, hash)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.LastUsed == nil {
		t.Fatalf("last_used must be populated after Touch")
	}
}

func TestLookupBearerExpiryReturnsRow(t *testing.T) {
	// The store-level lookup does NOT enforce expiry — that's the
	// middleware's job (so it can return 401 with the right shape).
	// This test pins the contract: store returns the row regardless of
	// expiry, leaving the gate to the API layer.
	ctx := context.Background()
	s := newStore(t)
	admin := seedUser(t, s, "admin@x", "admin")

	past := time.Now().UTC().Add(-time.Hour)
	view, plaintext, _ := s.MintAutomationToken(ctx, admin.ID, "admin", "a", []string{"tunnels:read:any"}, &past)
	if view.ExpiresAt == nil || !view.ExpiresAt.Before(time.Now().UTC()) {
		t.Fatalf("expected past expires_at, got %v", view.ExpiresAt)
	}

	sum := sha256.Sum256([]byte(plaintext))
	hash := hex.EncodeToString(sum[:])
	got, err := s.LookupBearer(ctx, hash)
	if err != nil {
		t.Fatalf("store-level lookup must succeed even when expired: %v", err)
	}
	if got.ID != view.ID {
		t.Fatalf("id mismatch")
	}
}

// --- helpers ---------------------------------------------------------------

func seedUser(t *testing.T, s *Store, email, role string) db.User {
	t.Helper()
	u, err := s.CreateUser(context.Background(), email, "password1", role)
	if err != nil {
		t.Fatalf("seed user %q: %v", email, err)
	}
	return u
}
