package db

import (
	"context"
	"database/sql"
	"fmt"
)

// ListRoles returns all roles ordered by name.
func (x *DB) ListRoles(ctx context.Context) ([]Role, error) {
	rows, err := x.sqlDB.QueryContext(ctx,
		`SELECT name, description, created_at FROM roles ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.Name, &r.Description, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list roles rows: %w", err)
	}
	return out, nil
}

// GetRole returns the role with the given name, or ErrNotFound.
func (x *DB) GetRole(ctx context.Context, name string) (Role, error) {
	var r Role
	err := x.sqlDB.QueryRowContext(ctx,
		`SELECT name, description, created_at FROM roles WHERE name=?`, name,
	).Scan(&r.Name, &r.Description, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return Role{}, ErrNotFound
	}
	if err != nil {
		return Role{}, fmt.Errorf("get role: %w", err)
	}
	return r, nil
}
