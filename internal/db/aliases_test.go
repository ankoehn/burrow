package db

import (
	"context"
	"errors"
	"testing"
)

// seedSvc inserts a services row for the given user + service ID via the
// services GetOrCreate path, returning the resolved service id. The test
// data layer does not currently expose a "create with explicit id" helper,
// so we use the (user, name) → id round-trip from GetOrCreateService and
// keep the produced id for the model_aliases rows.
func seedSvc(t *testing.T, x *DB, userID, name string) string {
	t.Helper()
	s, err := x.GetOrCreateService(context.Background(), userID, name, "http")
	if err != nil {
		t.Fatalf("seed service: %v", err)
	}
	return s.ID
}

func TestModelAliasesCRUD(t *testing.T) {
	x := testDB(t)
	ctx := context.Background()
	mustUser(t, x, "u1")
	svcA := seedSvc(t, x, "u1", "svc-a")
	svcB := seedSvc(t, x, "u1", "svc-b")

	// Empty list returns a non-nil empty slice.
	rows, err := x.ListModelAliases(ctx)
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if rows == nil || len(rows) != 0 {
		t.Fatalf("want empty non-nil slice, got %v", rows)
	}

	// Create + read-back.
	if err := x.CreateModelAlias(ctx, ModelAlias{
		Alias: "fast", ConcreteModel: "gpt-4o-mini", ServiceID: svcA,
	}); err != nil {
		t.Fatalf("create fast: %v", err)
	}
	if err := x.CreateModelAlias(ctx, ModelAlias{
		Alias: "smart", ConcreteModel: "gpt-4o", ServiceID: svcB,
	}); err != nil {
		t.Fatalf("create smart: %v", err)
	}
	got, err := x.GetModelAlias(ctx, "fast")
	if err != nil {
		t.Fatalf("get fast: %v", err)
	}
	if got.ConcreteModel != "gpt-4o-mini" || got.ServiceID != svcA {
		t.Fatalf("got %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Errorf("created_at zero (sqlite default not applied?)")
	}

	// List returns both, ordered by alias.
	rows, err = x.ListModelAliases(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 || rows[0].Alias != "fast" || rows[1].Alias != "smart" {
		t.Fatalf("list order: %+v", rows)
	}

	// Update: replaces concrete + service.
	if err := x.UpdateModelAlias(ctx, "fast", "gpt-3.5", svcB); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = x.GetModelAlias(ctx, "fast")
	if got.ConcreteModel != "gpt-3.5" || got.ServiceID != svcB {
		t.Fatalf("update did not persist: %+v", got)
	}

	// Delete removes; subsequent get → ErrNotFound.
	if err := x.DeleteModelAlias(ctx, "fast"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := x.GetModelAlias(ctx, "fast"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete: %v, want ErrNotFound", err)
	}

	// Delete unknown alias → ErrNotFound.
	if err := x.DeleteModelAlias(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete unknown: %v, want ErrNotFound", err)
	}
	// Update unknown alias → ErrNotFound.
	if err := x.UpdateModelAlias(ctx, "nope", "x", svcA); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update unknown: %v, want ErrNotFound", err)
	}
}

func TestModelAliasesCreateConflict(t *testing.T) {
	x := testDB(t)
	ctx := context.Background()
	mustUser(t, x, "u1")
	svc := seedSvc(t, x, "u1", "svc")

	if err := x.CreateModelAlias(ctx, ModelAlias{
		Alias: "dup", ConcreteModel: "gpt-4o", ServiceID: svc,
	}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	err := x.CreateModelAlias(ctx, ModelAlias{
		Alias: "dup", ConcreteModel: "other", ServiceID: svc,
	})
	if !errors.Is(err, ErrAliasExists) {
		t.Fatalf("second create: %v, want ErrAliasExists", err)
	}
}

func TestModelAliasesCascadeOnServiceDelete(t *testing.T) {
	x := testDB(t)
	ctx := context.Background()
	mustUser(t, x, "u1")
	svc := seedSvc(t, x, "u1", "svc")
	if err := x.CreateModelAlias(ctx, ModelAlias{
		Alias: "a", ConcreteModel: "m", ServiceID: svc,
	}); err != nil {
		t.Fatal(err)
	}
	// Cascade delete via services.user_id → users(id) ON DELETE CASCADE.
	if err := x.DeleteUser(ctx, "u1"); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if _, err := x.GetModelAlias(ctx, "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("alias should cascade away with service; got %v", err)
	}
}
