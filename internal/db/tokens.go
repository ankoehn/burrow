package db

import (
	"context"
	"database/sql"
	"fmt"
)

// CreateClientToken inserts a new client token row.
func (x *DB) CreateClientToken(ctx context.Context, ct ClientToken) error {
	_, err := x.sqlDB.ExecContext(ctx,
		`INSERT INTO client_tokens(id, user_id, name, token_hash) VALUES(?,?,?,?)`,
		ct.ID, ct.UserID, ct.Name, ct.TokenHash,
	)
	if err != nil {
		return fmt.Errorf("create client token: %w", err)
	}
	return nil
}

// GetClientTokenByHash returns the token whose token_hash matches, or ErrNotFound.
func (x *DB) GetClientTokenByHash(ctx context.Context, hash string) (ClientToken, error) {
	var ct ClientToken
	var lastUsed sql.NullTime
	err := x.sqlDB.QueryRowContext(ctx,
		`SELECT id, user_id, name, token_hash, last_used, created_at FROM client_tokens WHERE token_hash=?`, hash,
	).Scan(&ct.ID, &ct.UserID, &ct.Name, &ct.TokenHash, &lastUsed, &ct.CreatedAt)
	if err == sql.ErrNoRows {
		return ClientToken{}, ErrNotFound
	}
	if err != nil {
		return ClientToken{}, fmt.Errorf("get client token by hash: %w", err)
	}
	if lastUsed.Valid {
		ct.LastUsed = &lastUsed.Time
	}
	return ct, nil
}

// ListClientTokensByUser returns all tokens belonging to the given user.
func (x *DB) ListClientTokensByUser(ctx context.Context, userID string) ([]ClientToken, error) {
	rows, err := x.sqlDB.QueryContext(ctx,
		`SELECT id, user_id, name, token_hash, last_used, created_at FROM client_tokens WHERE user_id=? ORDER BY created_at`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list client tokens: %w", err)
	}
	defer rows.Close()

	var out []ClientToken
	for rows.Next() {
		var ct ClientToken
		var lastUsed sql.NullTime
		if err := rows.Scan(&ct.ID, &ct.UserID, &ct.Name, &ct.TokenHash, &lastUsed, &ct.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan client token: %w", err)
		}
		if lastUsed.Valid {
			ct.LastUsed = &lastUsed.Time
		}
		out = append(out, ct)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list client tokens rows: %w", err)
	}
	return out, nil
}

// TouchClientTokenLastUsed sets last_used to the current timestamp for the given token ID.
func (x *DB) TouchClientTokenLastUsed(ctx context.Context, id string) error {
	_, err := x.sqlDB.ExecContext(ctx,
		`UPDATE client_tokens SET last_used=CURRENT_TIMESTAMP WHERE id=?`, id,
	)
	if err != nil {
		return fmt.Errorf("touch client token last_used: %w", err)
	}
	return nil
}

// DeleteClientToken removes the token with the given ID, scoped to the owning user.
// Returns ErrNotFound if no row matched (token does not exist or belongs to another user).
func (x *DB) DeleteClientToken(ctx context.Context, id, userID string) error {
	res, err := x.sqlDB.ExecContext(ctx,
		`DELETE FROM client_tokens WHERE id=? AND user_id=?`, id, userID,
	)
	if err != nil {
		return fmt.Errorf("delete client token: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete client token rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("delete client token: %w", ErrNotFound)
	}
	return nil
}
