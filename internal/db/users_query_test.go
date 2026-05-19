package db

import (
	"context"
	"testing"
)

func seedUser(t *testing.T, x *DB, id, email, role string) {
	t.Helper()
	if err := x.CreateUser(context.Background(), User{ID: id, Email: email, PasswordHash: "h", Role: role}); err != nil {
		t.Fatal(err)
	}
}

func TestUserStatusRoleAndLastLogin(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)
	seedUser(t, x, "u1", "a@b.c", "user")

	u, err := x.GetUserByID(ctx, "u1")
	if err != nil || u.Status != "active" || u.LastLogin != nil {
		t.Fatalf("fresh user: %+v %v", u, err)
	}

	if err := x.UpdateUserRole(ctx, "u1", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := x.UpdateUserStatus(ctx, "u1", "suspended"); err != nil {
		t.Fatal(err)
	}
	if err := x.TouchUserLastLogin(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	u, _ = x.GetUserByID(ctx, "u1")
	if u.Role != "admin" || u.Status != "suspended" || u.LastLogin == nil {
		t.Fatalf("after updates: %+v", u)
	}

	if err := x.UpdateUserRole(ctx, "missing", "user"); err != ErrNotFound {
		t.Fatalf("UpdateUserRole missing: want ErrNotFound, got %v", err)
	}
	if err := x.UpdateUserStatus(ctx, "missing", "active"); err != ErrNotFound {
		t.Fatalf("UpdateUserStatus missing: want ErrNotFound, got %v", err)
	}
}

func TestListUsersPage(t *testing.T) {
	ctx := context.Background()
	x := testDB(t)
	seedUser(t, x, "u1", "alice@example.com", "admin")
	seedUser(t, x, "u2", "bob@example.com", "user")
	seedUser(t, x, "u3", "carol@other.com", "user")

	all, total, err := x.ListUsersPage(ctx, "", 0, 0)
	if err != nil || total != 3 || len(all) != 3 {
		t.Fatalf("page all: n=%d total=%d err=%v", len(all), total, err)
	}
	// search by email substring
	got, total, err := x.ListUsersPage(ctx, "example.com", 0, 0)
	if err != nil || total != 2 || len(got) != 2 {
		t.Fatalf("search: n=%d total=%d err=%v", len(got), total, err)
	}
	// limit + offset, total still reflects full filtered count
	got, total, err = x.ListUsersPage(ctx, "", 2, 2)
	if err != nil || total != 3 || len(got) != 1 {
		t.Fatalf("paged: n=%d total=%d err=%v", len(got), total, err)
	}
}
