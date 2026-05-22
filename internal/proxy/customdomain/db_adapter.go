package customdomain

import (
	"context"
)

// FuncDBStore is a DBStore implementation backed by a plain function.
// Callers (API layer, cmd/server) construct one by wrapping a closure that
// calls the real *db.DB and translates db.ServiceCustomDomain → DomainRow.
// This avoids an import cycle: customdomain never imports db.
type FuncDBStore struct {
	Fn func(ctx context.Context, hostname string) (DomainRow, bool, error)
}

// LookupCustomDomainByHostname delegates to the wrapped function.
func (f *FuncDBStore) LookupCustomDomainByHostname(ctx context.Context, hostname string) (DomainRow, bool, error) {
	return f.Fn(ctx, hostname)
}
