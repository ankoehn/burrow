package db

import (
	"context"
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

	r, err := x.GetRole(ctx, "user")
	if err != nil || r.Name != "user" {
		t.Fatalf("GetRole(user): %+v %v", r, err)
	}
	if _, err := x.GetRole(ctx, "nope"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
