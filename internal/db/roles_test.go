package db

import (
	"context"
	"errors"
	"testing"
)

func TestListAndGetRoles(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)

	roles, err := x.ListRoles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 2 {
		t.Fatalf("want 2 roles, got %d", len(roles))
	}
	// ordered by name: admin, user
	if roles[0].Name != "admin" || roles[1].Name != "user" {
		t.Fatalf("unexpected order: %+v", roles)
	}
	if roles[0].Description == "" || roles[0].CreatedAt.IsZero() {
		t.Fatalf("admin role incomplete: %+v", roles[0])
	}
	// migration 0005: admin/user must be flagged builtin=true and start
	// with an empty permissions JSON array (the closed permission catalog
	// lives in authz, not the DB row, for builtin roles).
	for _, r := range roles {
		if !r.Builtin {
			t.Fatalf("%s: want Builtin=true, got false", r.Name)
		}
		if r.Permissions == nil || len(r.Permissions) != 0 {
			t.Fatalf("%s: want empty Permissions, got %v", r.Name, r.Permissions)
		}
		if r.DefaultForNewUsers {
			t.Fatalf("%s: builtin must not be marked default initially", r.Name)
		}
	}

	r, err := x.GetRole(ctx, "user")
	if err != nil || r.Name != "user" {
		t.Fatalf("GetRole(user): %+v %v", r, err)
	}
	if !r.Builtin {
		t.Fatal("user role must be Builtin")
	}
	if _, err := x.GetRole(ctx, "nope"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// TestRoleCreate covers a happy-path POST: a non-builtin role with two
// permissions and default_for_new_users=true; the prior default (none) is
// a no-op, and the row reads back with the expected shape.
func TestRoleCreate(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)

	if err := x.CreateRole(ctx, "analyst", "AI analyst",
		[]string{"ai:read:any", "audit:read"}, true); err != nil {
		t.Fatalf("create analyst: %v", err)
	}
	r, err := x.GetRole(ctx, "analyst")
	if err != nil {
		t.Fatal(err)
	}
	if r.Builtin {
		t.Fatal("created role must NOT be Builtin")
	}
	if !r.DefaultForNewUsers {
		t.Fatal("DefaultForNewUsers must be true")
	}
	if len(r.Permissions) != 2 {
		t.Fatalf("want 2 perms, got %v", r.Permissions)
	}
	def, err := x.DefaultRoleName(ctx)
	if err != nil || def != "analyst" {
		t.Fatalf("DefaultRoleName: name=%q err=%v", def, err)
	}

	// Duplicate name → ErrRoleExists.
	if err := x.CreateRole(ctx, "analyst", "dup", nil, false); !errors.Is(err, ErrRoleExists) {
		t.Fatalf("want ErrRoleExists, got %v", err)
	}
}

// TestRoleCreateDefaultClearsPrior — creating a second default-for-new-users
// role atomically clears the prior default in the same transaction.
func TestRoleCreateDefaultClearsPrior(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)

	if err := x.CreateRole(ctx, "first", "", nil, true); err != nil {
		t.Fatal(err)
	}
	if err := x.CreateRole(ctx, "second", "", nil, true); err != nil {
		t.Fatal(err)
	}
	def, err := x.DefaultRoleName(ctx)
	if err != nil || def != "second" {
		t.Fatalf("want default=second, got %q (err=%v)", def, err)
	}
	// The first role must have been cleared.
	r, _ := x.GetRole(ctx, "first")
	if r.DefaultForNewUsers {
		t.Fatal("first role must no longer be default")
	}
}

// TestRoleUpdate covers a successful patch, builtin guard, and not-found.
func TestRoleUpdate(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)

	// Builtin guard: PUT admin → ErrRoleBuiltin.
	desc := "tampered"
	if err := x.UpdateRole(ctx, "admin", RoleUpdate{Description: &desc}); !errors.Is(err, ErrRoleBuiltin) {
		t.Fatalf("want ErrRoleBuiltin, got %v", err)
	}
	if err := x.UpdateRole(ctx, "ghost", RoleUpdate{Description: &desc}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	if err := x.CreateRole(ctx, "analyst", "v1", []string{"audit:read"}, false); err != nil {
		t.Fatal(err)
	}
	newPerms := []string{"ai:read:any"}
	newDesc := "v2"
	yes := true
	if err := x.UpdateRole(ctx, "analyst", RoleUpdate{
		Description:        &newDesc,
		Permissions:        &newPerms,
		DefaultForNewUsers: &yes,
	}); err != nil {
		t.Fatalf("update analyst: %v", err)
	}
	r, err := x.GetRole(ctx, "analyst")
	if err != nil {
		t.Fatal(err)
	}
	if r.Description != "v2" || len(r.Permissions) != 1 || r.Permissions[0] != "ai:read:any" {
		t.Fatalf("update did not persist: %+v", r)
	}
	if !r.DefaultForNewUsers {
		t.Fatal("DefaultForNewUsers must be true after update")
	}
}

// TestRoleDeleteCascade — deleting a custom role re-assigns affected users
// to the fallback role in a single transaction and returns the affected IDs.
func TestRoleDeleteCascade(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)

	if err := x.CreateRole(ctx, "analyst", "", []string{"audit:read"}, false); err != nil {
		t.Fatal(err)
	}
	// Two users on the analyst role, one on user.
	for _, id := range []string{"u-a", "u-b"} {
		if err := x.CreateUser(ctx, User{ID: id, Email: id + "@x", PasswordHash: "h", Role: "analyst"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := x.CreateUser(ctx, User{ID: "u-c", Email: "u-c@x", PasswordHash: "h", Role: "user"}); err != nil {
		t.Fatal(err)
	}

	// Builtin guard.
	if _, err := x.DeleteRole(ctx, "admin", "user"); !errors.Is(err, ErrRoleBuiltin) {
		t.Fatalf("want ErrRoleBuiltin, got %v", err)
	}

	affected, err := x.DeleteRole(ctx, "analyst", "user")
	if err != nil {
		t.Fatalf("delete analyst: %v", err)
	}
	if len(affected) != 2 {
		t.Fatalf("want 2 affected users, got %v", affected)
	}
	// Verify the re-assignment is durable and exactly the affected set.
	for _, id := range []string{"u-a", "u-b"} {
		u, err := x.GetUserByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if u.Role != "user" {
			t.Fatalf("user %s: role=%q want 'user'", id, u.Role)
		}
	}
	u, _ := x.GetUserByID(ctx, "u-c")
	if u.Role != "user" {
		t.Fatalf("uninvolved user must keep its role, got %q", u.Role)
	}
	if _, err := x.GetRole(ctx, "analyst"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("analyst row must be gone, got %v", err)
	}

	// Delete on missing role → ErrNotFound.
	if _, err := x.DeleteRole(ctx, "ghost", "user"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
