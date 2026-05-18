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

// UpdateUserPassword sets a new password_hash for the given user ID.
// Returns ErrNotFound if no row matched.
func (x *DB) UpdateUserPassword(ctx context.Context, userID, newHash string) error {
	res, err := x.sqlDB.ExecContext(ctx,
		`UPDATE users SET password_hash=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		newHash, userID,
	)
	if err != nil {
		return fmt.Errorf("update user password: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update user password rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("update user password: %w", ErrNotFound)
	}
	return nil
}

// ListUsers returns all users (id, email, role, created_at) — never password_hash.
func (x *DB) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := x.sqlDB.QueryContext(ctx,
		`SELECT id, email, role, created_at FROM users ORDER BY created_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Role, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list users rows: %w", err)
	}
	return out, nil
}

// DeleteUser removes the user with the given ID.
// ON DELETE CASCADE removes all associated sessions, tokens, and tunnels.
// Returns ErrNotFound if no row matched.
func (x *DB) DeleteUser(ctx context.Context, id string) error {
	res, err := x.sqlDB.ExecContext(ctx, `DELETE FROM users WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete user rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("delete user: %w", ErrNotFound)
	}
	return nil
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
