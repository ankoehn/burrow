package db

import (
	"context"
	"database/sql"
	"fmt"
)

// UpsertTunnel inserts a tunnel row or, on conflict with an existing ID,
// updates its name, type, remote_port, and local_addr.
func (x *DB) UpsertTunnel(ctx context.Context, tn Tunnel) error {
	_, err := x.sqlDB.ExecContext(ctx,
		`INSERT INTO tunnels(id, user_id, name, type, remote_port, local_addr)
		 VALUES(?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET
		   name=excluded.name,
		   type=excluded.type,
		   remote_port=excluded.remote_port,
		   local_addr=excluded.local_addr`,
		tn.ID, tn.UserID, tn.Name, tn.Type, tn.RemotePort, tn.LocalAddr,
	)
	if err != nil {
		return fmt.Errorf("upsert tunnel: %w", err)
	}
	return nil
}

// TouchTunnelLastSeen sets last_seen to the current timestamp for the given tunnel ID.
func (x *DB) TouchTunnelLastSeen(ctx context.Context, tunnelID string) error {
	_, err := x.sqlDB.ExecContext(ctx,
		`UPDATE tunnels SET last_seen=CURRENT_TIMESTAMP WHERE id=?`, tunnelID,
	)
	if err != nil {
		return fmt.Errorf("touch tunnel last_seen: %w", err)
	}
	return nil
}

// ListTunnelsByUser returns all tunnel rows belonging to the given user.
func (x *DB) ListTunnelsByUser(ctx context.Context, userID string) ([]Tunnel, error) {
	rows, err := x.sqlDB.QueryContext(ctx,
		`SELECT id, user_id, name, type, remote_port, local_addr, created_at, last_seen,
		        total_bytes_in, total_bytes_out, last_flushed_at, access_mode
		   FROM tunnels WHERE user_id=? ORDER BY created_at`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list tunnels: %w", err)
	}
	defer rows.Close()

	var out []Tunnel
	for rows.Next() {
		tn, err := scanTunnelRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, tn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tunnels rows: %w", err)
	}
	return out, nil
}

// scanTunnelRows scans one full tunnel row (shared by list/get).
func scanTunnelRows(rows *sql.Rows) (Tunnel, error) {
	var tn Tunnel
	var lastSeen, lastFlushed sql.NullTime
	if err := rows.Scan(
		&tn.ID, &tn.UserID, &tn.Name, &tn.Type, &tn.RemotePort, &tn.LocalAddr,
		&tn.CreatedAt, &lastSeen, &tn.TotalBytesIn, &tn.TotalBytesOut,
		&lastFlushed, &tn.AccessMode,
	); err != nil {
		return Tunnel{}, fmt.Errorf("scan tunnel: %w", err)
	}
	if lastSeen.Valid {
		tn.LastSeen = &lastSeen.Time
	}
	if lastFlushed.Valid {
		tn.LastFlushedAt = &lastFlushed.Time
	}
	return tn, nil
}

// GetTunnel returns one tunnel row by ID, or ErrNotFound.
func (x *DB) GetTunnel(ctx context.Context, id string) (Tunnel, error) {
	var tn Tunnel
	var lastSeen, lastFlushed sql.NullTime
	err := x.sqlDB.QueryRowContext(ctx,
		`SELECT id, user_id, name, type, remote_port, local_addr, created_at, last_seen,
		        total_bytes_in, total_bytes_out, last_flushed_at, access_mode
		   FROM tunnels WHERE id=?`, id,
	).Scan(
		&tn.ID, &tn.UserID, &tn.Name, &tn.Type, &tn.RemotePort, &tn.LocalAddr,
		&tn.CreatedAt, &lastSeen, &tn.TotalBytesIn, &tn.TotalBytesOut,
		&lastFlushed, &tn.AccessMode,
	)
	if err == sql.ErrNoRows {
		return Tunnel{}, ErrNotFound
	}
	if err != nil {
		return Tunnel{}, fmt.Errorf("get tunnel: %w", err)
	}
	if lastSeen.Valid {
		tn.LastSeen = &lastSeen.Time
	}
	if lastFlushed.Valid {
		tn.LastFlushedAt = &lastFlushed.Time
	}
	return tn, nil
}

// FlushTunnelTotals atomically adds the given deltas to the persisted byte
// counters and stamps last_flushed_at. Missing rows are a no-op (a tunnel may
// be flushed after its row was cascade-deleted with the owning user).
func (x *DB) FlushTunnelTotals(ctx context.Context, id string, addIn, addOut int64) error {
	_, err := x.sqlDB.ExecContext(ctx,
		`UPDATE tunnels
		    SET total_bytes_in  = total_bytes_in  + ?,
		        total_bytes_out = total_bytes_out + ?,
		        last_flushed_at = CURRENT_TIMESTAMP
		  WHERE id=?`,
		addIn, addOut, id,
	)
	if err != nil {
		return fmt.Errorf("flush tunnel totals: %w", err)
	}
	return nil
}

// SetTunnelAccessMode sets a tunnel's access_mode scoped to its owner.
// Returns ErrNotFound if no row matched. Enum validation is the caller's
// responsibility (store.SetTunnelAccessMode).
func (x *DB) SetTunnelAccessMode(ctx context.Context, id, userID, mode string) error {
	res, err := x.sqlDB.ExecContext(ctx,
		`UPDATE tunnels SET access_mode=? WHERE id=? AND user_id=?`, mode, id, userID)
	if err != nil {
		return fmt.Errorf("set tunnel access mode: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set tunnel access mode rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
