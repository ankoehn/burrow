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
		`SELECT id, user_id, name, type, remote_port, local_addr, created_at, last_seen
		 FROM tunnels WHERE user_id=? ORDER BY created_at`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list tunnels: %w", err)
	}
	defer rows.Close()

	var out []Tunnel
	for rows.Next() {
		var tn Tunnel
		var lastSeen sql.NullTime
		if err := rows.Scan(
			&tn.ID, &tn.UserID, &tn.Name, &tn.Type,
			&tn.RemotePort, &tn.LocalAddr, &tn.CreatedAt, &lastSeen,
		); err != nil {
			return nil, fmt.Errorf("scan tunnel: %w", err)
		}
		if lastSeen.Valid {
			tn.LastSeen = &lastSeen.Time
		}
		out = append(out, tn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tunnels rows: %w", err)
	}
	return out, nil
}
