package store

import (
	"context"
	"errors"
	"testing"

	"github.com/ankoehn/burrow/internal/authz"
	"github.com/ankoehn/burrow/internal/db"
)

// TestRoleCRUDHappyPath covers create → list → get → update → delete with
// the authz cache assertion threaded through: after CreateRole, Can() must
// recognise the new role's permissions; after DeleteRole, it must not.
func TestRoleCRUDHappyPath(t *testing.T) {
	defer authz.SetRoles(nil) // restore clean cache for parallel tests
	ctx := context.Background()
	s := testStore(t)

	if err := s.CreateRole(ctx, "analyst", "AI analyst",
		[]string{"tunnels:read:any", "ai:read:any"}, false); err != nil {
		t.Fatalf("create analyst: %v", err)
	}
	// Cache populated → Can() sees it.
	if !authz.Can("analyst", authz.PermAIReadAny) {
		t.Fatal("authz.Can(analyst, ai:read:any) must be true after CreateRole")
	}
	if !authz.Can("analyst", authz.PermTunnelsReadAny) {
		t.Fatal("authz.Can(analyst, tunnels:read:any) must be true after CreateRole")
	}

	// List includes analyst alongside the two builtins.
	rows, err := s.ListRoles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 roles, got %d", len(rows))
	}

	// Detail surfaces the v0.4.0 fields.
	rd, err := s.GetRole(ctx, "analyst")
	if err != nil {
		t.Fatal(err)
	}
	if rd.Builtin {
		t.Fatal("analyst must not be Builtin")
	}
	if len(rd.Permissions) != 2 {
		t.Fatalf("want 2 perms, got %v", rd.Permissions)
	}

	// Patch the description and shrink the permission set.
	newDesc := "AI reviewer"
	newPerms := []string{"ai:read:any"}
	if err := s.UpdateRole(ctx, "analyst", RoleUpdate{
		Description: &newDesc,
		Permissions: &newPerms,
	}); err != nil {
		t.Fatalf("update analyst: %v", err)
	}
	// Cache reflects the shrink: tunnels:read:any no longer held.
	if authz.Can("analyst", authz.PermTunnelsReadAny) {
		t.Fatal("authz.Can(analyst, tunnels:read:any) must be false after shrink")
	}
	if !authz.Can("analyst", authz.PermAIReadAny) {
		t.Fatal("authz.Can(analyst, ai:read:any) must remain true")
	}

	// Delete clears the cache.
	if _, err := s.DeleteRole(ctx, "analyst"); err != nil {
		t.Fatalf("delete analyst: %v", err)
	}
	if authz.Can("analyst", authz.PermAIReadAny) {
		t.Fatal("authz.Can(analyst, *) must be false after DeleteRole")
	}
}

// TestRoleCreateRejectsBuiltinName ensures CreateRole cannot shadow admin/user
// (the DB enforces this via the PK UNIQUE on name; we surface it as
// ErrRoleExists which the handler maps to 409).
func TestRoleCreateRejectsBuiltinName(t *testing.T) {
	defer authz.SetRoles(nil)
	ctx := context.Background()
	s := testStore(t)
	if err := s.CreateRole(ctx, "admin", "fake", nil, false); !errors.Is(err, ErrRoleExists) {
		t.Fatalf("want ErrRoleExists, got %v", err)
	}
}

// TestRoleUpdateBuiltinRefused proves admin/user remain read-only.
func TestRoleUpdateBuiltinRefused(t *testing.T) {
	defer authz.SetRoles(nil)
	ctx := context.Background()
	s := testStore(t)
	desc := "tampered"
	if err := s.UpdateRole(ctx, "admin", RoleUpdate{Description: &desc}); !errors.Is(err, ErrRoleBuiltin) {
		t.Fatalf("want ErrRoleBuiltin, got %v", err)
	}
	if err := s.UpdateRole(ctx, "user", RoleUpdate{Description: &desc}); !errors.Is(err, ErrRoleBuiltin) {
		t.Fatalf("want ErrRoleBuiltin, got %v", err)
	}
	if _, err := s.DeleteRole(ctx, "user"); !errors.Is(err, ErrRoleBuiltin) {
		t.Fatalf("delete builtin: want ErrRoleBuiltin, got %v", err)
	}
}

// TestRoleUnknownPermission proves both Create and Update reject any key
// outside the closed authz catalog, and that the rejected key bubbles up in
// the error message so the handler can echo it back verbatim.
func TestRoleUnknownPermission(t *testing.T) {
	defer authz.SetRoles(nil)
	ctx := context.Background()
	s := testStore(t)

	err := s.CreateRole(ctx, "rogue", "", []string{"ai:read:any", "nope:perm"}, false)
	var unk ErrUnknownPermission
	if !errors.As(err, &unk) {
		t.Fatalf("want ErrUnknownPermission, got %v", err)
	}
	if unk.Key != "nope:perm" {
		t.Fatalf("want offending key 'nope:perm', got %q", unk.Key)
	}

	if err := s.CreateRole(ctx, "analyst", "", []string{"ai:read:any"}, false); err != nil {
		t.Fatal(err)
	}
	bad := []string{"also:bogus"}
	err = s.UpdateRole(ctx, "analyst", RoleUpdate{Permissions: &bad})
	if !errors.As(err, &unk) || unk.Key != "also:bogus" {
		t.Fatalf("UpdateRole: want ErrUnknownPermission{Key:also:bogus}, got %v", err)
	}
}

// TestRoleDefaultForNewUsersClearsPrior — POST/PUT with
// default_for_new_users=true atomically swap the default (single tx).
func TestRoleDefaultForNewUsersClearsPrior(t *testing.T) {
	defer authz.SetRoles(nil)
	ctx := context.Background()
	s := testStore(t)

	if err := s.CreateRole(ctx, "first", "", nil, true); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRole(ctx, "second", "", nil, true); err != nil {
		t.Fatal(err)
	}
	r1, _ := s.GetRole(ctx, "first")
	r2, _ := s.GetRole(ctx, "second")
	if r1.DefaultForNewUsers {
		t.Fatal("first must lose default after second became default")
	}
	if !r2.DefaultForNewUsers {
		t.Fatal("second must be the current default")
	}

	yes := true
	if err := s.UpdateRole(ctx, "first", RoleUpdate{DefaultForNewUsers: &yes}); err != nil {
		t.Fatal(err)
	}
	r1, _ = s.GetRole(ctx, "first")
	r2, _ = s.GetRole(ctx, "second")
	if !r1.DefaultForNewUsers || r2.DefaultForNewUsers {
		t.Fatalf("default did not swap: first=%v second=%v", r1.DefaultForNewUsers, r2.DefaultForNewUsers)
	}
}

// TestRoleDeleteCascadeReassigns — deleting a role used by users cascades
// them onto the current default-for-new-users role (or "user" if no default
// is configured). The store returns the affected user IDs for audit (Task 25).
func TestRoleDeleteCascadeReassigns(t *testing.T) {
	defer authz.SetRoles(nil)
	ctx := context.Background()
	s, _ := newStoreWithDB(t)

	// Create custom role + two users on it; mark "user" as the default so
	// the cascade has a clear target distinct from "admin".
	if err := s.CreateRole(ctx, "analyst", "", []string{"ai:read:any"}, false); err != nil {
		t.Fatal(err)
	}
	yes := true
	if err := s.UpdateRole(ctx, "user", RoleUpdate{DefaultForNewUsers: &yes}); err == nil {
		t.Fatal("update builtin must fail (ErrRoleBuiltin)")
	}
	// Builtin can't be defaulted via UpdateRole — fall back to the safe
	// default fallback inside DeleteRole (which routes to "user" when the
	// default role is absent or is the role being deleted).
	u1, err := s.CreateUser(ctx, "a1@x", "password1", "user")
	if err != nil {
		t.Fatal(err)
	}
	// Manually push the user onto the analyst role (bypassing the role
	// allowlist guard in UpdateUserRole, which is hardcoded to admin/user).
	if _, err := s.q.DB().ExecContext(ctx,
		`UPDATE users SET role='analyst' WHERE id=?`, u1.ID,
	); err != nil {
		t.Fatal(err)
	}

	affected, err := s.DeleteRole(ctx, "analyst")
	if err != nil {
		t.Fatalf("delete analyst: %v", err)
	}
	if len(affected) != 1 || affected[0] != u1.ID {
		t.Fatalf("affected = %v want [%s]", affected, u1.ID)
	}
	got, err := s.GetUserByID(ctx, u1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != "user" {
		t.Fatalf("user must be re-assigned to 'user', got %q", got.Role)
	}
	if _, err := s.GetRole(ctx, "analyst"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("analyst row must be gone, got %v", err)
	}
}
