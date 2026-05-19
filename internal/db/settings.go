package db

import (
	"context"
	"fmt"
)

// GetAllSettings returns every settings row ordered by key.
func (x *DB) GetAllSettings(ctx context.Context) ([]Setting, error) {
	rows, err := x.sqlDB.QueryContext(ctx,
		`SELECT key, value, updated_at FROM settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	defer rows.Close()
	var out []Setting
	for rows.Next() {
		var s Setting
		if err := rows.Scan(&s.Key, &s.Value, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list settings rows: %w", err)
	}
	return out, nil
}

// SetSettings upserts every key/value pair in a single transaction.
func (x *DB) SetSettings(ctx context.Context, kv map[string]string) error {
	if len(kv) == 0 {
		return nil
	}
	tx, err := x.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin settings tx: %w", err)
	}
	for k, v := range kv {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO settings(key, value, updated_at) VALUES(?,?,CURRENT_TIMESTAMP)
			 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`,
			k, v,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("upsert setting %q: %w", k, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit settings tx: %w", err)
	}
	return nil
}
