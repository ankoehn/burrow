package db

import "testing"

func TestBackendPlaceholderSQLite(t *testing.T) {
	b, err := OpenSQLite(":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	defer b.DB().Close()

	if got := b.Placeholder(1); got != "?" {
		t.Errorf("Placeholder(1) = %q; want %q", got, "?")
	}
	if got := b.Now(); got != "CURRENT_TIMESTAMP" {
		t.Errorf("Now() = %q; want %q", got, "CURRENT_TIMESTAMP")
	}
	if got := b.Driver(); got != "sqlite" {
		t.Errorf("Driver() = %q; want %q", got, "sqlite")
	}
}

// TestDBSatisfiesBackend verifies at compile time that *DB satisfies Backend.
func TestDBSatisfiesBackend(t *testing.T) {
	var _ Backend = (*DB)(nil)
	// Also verify the methods return expected SQLite values.
	x := testDB(t)
	if got := x.Driver(); got != "sqlite" {
		t.Errorf("DB.Driver() = %q; want %q", got, "sqlite")
	}
	if got := x.Now(); got != "CURRENT_TIMESTAMP" {
		t.Errorf("DB.Now() = %q; want %q", got, "CURRENT_TIMESTAMP")
	}
	if got := x.Placeholder(1); got != "?" {
		t.Errorf("DB.Placeholder(1) = %q; want %q", got, "?")
	}
	if got := x.Placeholder(5); got != "?" {
		t.Errorf("DB.Placeholder(5) = %q; want %q", got, "?")
	}
}
