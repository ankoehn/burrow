package db

import (
	"context"
	"database/sql"
	"fmt"
)

// GetUpstreamCredential returns the upstream-credential binding for the given
// serviceID. Returns ErrNotFound (nil wrapped) when no row exists.
func (x *DB) GetUpstreamCredential(ctx context.Context, serviceID string) (ServiceUpstreamCredential, error) {
	var c ServiceUpstreamCredential
	err := x.sqlDB.QueryRowContext(ctx,
		`SELECT service_id, slot, header_name, header_format, created_at, updated_at
		   FROM service_upstream_credentials WHERE service_id=?`,
		serviceID,
	).Scan(&c.ServiceID, &c.Slot, &c.HeaderName, &c.HeaderFormat, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return ServiceUpstreamCredential{}, ErrNotFound
	}
	if err != nil {
		return ServiceUpstreamCredential{}, fmt.Errorf("get upstream credential: %w", err)
	}
	return c, nil
}

// UpsertUpstreamCredential inserts or replaces the upstream-credential binding
// for the given service. The created_at is preserved on conflict (ON CONFLICT
// DO UPDATE only updates mutable columns).
func (x *DB) UpsertUpstreamCredential(ctx context.Context, c ServiceUpstreamCredential) error {
	_, err := x.sqlDB.ExecContext(ctx,
		`INSERT INTO service_upstream_credentials(service_id, slot, header_name, header_format)
		 VALUES(?,?,?,?)
		 ON CONFLICT(service_id) DO UPDATE SET
		   slot=excluded.slot,
		   header_name=excluded.header_name,
		   header_format=excluded.header_format,
		   updated_at=CURRENT_TIMESTAMP`,
		c.ServiceID, c.Slot, c.HeaderName, c.HeaderFormat,
	)
	if err != nil {
		return fmt.Errorf("upsert upstream credential: %w", err)
	}
	return nil
}

// DeleteUpstreamCredential removes the upstream-credential binding for the
// given serviceID. It is not an error if no row matched.
func (x *DB) DeleteUpstreamCredential(ctx context.Context, serviceID string) error {
	_, err := x.sqlDB.ExecContext(ctx,
		`DELETE FROM service_upstream_credentials WHERE service_id=?`, serviceID,
	)
	if err != nil {
		return fmt.Errorf("delete upstream credential: %w", err)
	}
	return nil
}
