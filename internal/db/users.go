package db

import (
	"context"
	"database/sql"
	"fmt"
)

// CountUsers returns the total number of users in the database.
func (x *DB) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := x.sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

// CreateUser inserts a new user row. Returns an error if the email already exists.
func (x *DB) CreateUser(ctx context.Context, u User) error {
	_, err := x.sqlDB.ExecContext(ctx,
		`INSERT INTO users(id, email, password_hash, role) VALUES(?,?,?,?)`,
		u.ID, u.Email, u.PasswordHash, u.Role,
	)
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

// GetUserByEmail returns the user with the given email, or ErrNotFound.
func (x *DB) GetUserByEmail(ctx context.Context, email string) (User, error) {
	return x.scanUser(x.sqlDB.QueryRowContext(ctx,
		`SELECT id, email, password_hash, role, created_at, updated_at FROM users WHERE email=?`, email,
	))
}

// GetUserByID returns the user with the given ID, or ErrNotFound.
func (x *DB) GetUserByID(ctx context.Context, id string) (User, error) {
	return x.scanUser(x.sqlDB.QueryRowContext(ctx,
		`SELECT id, email, password_hash, role, created_at, updated_at FROM users WHERE id=?`, id,
	))
}

// scanUser scans a single user row from a QueryRow result.
func (x *DB) scanUser(row *sql.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("scan user: %w", err)
	}
	return u, nil
}
