package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// CreateSession inserts a new session row.
func (x *DB) CreateSession(ctx context.Context, s Session) error {
	_, err := x.sqlDB.ExecContext(ctx,
		`INSERT INTO sessions(id, user_id, expires_at, user_agent, ip) VALUES(?,?,?,?,?)`,
		s.ID, s.UserID, s.ExpiresAt, s.UserAgent, s.IP,
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// GetSession returns the session with the given ID, or ErrNotFound.
// Note: the returned session may be expired; callers must check ExpiresAt.
func (x *DB) GetSession(ctx context.Context, id string) (Session, error) {
	var s Session
	err := x.sqlDB.QueryRowContext(ctx,
		`SELECT id, user_id, expires_at, created_at, COALESCE(user_agent,''), COALESCE(ip,'') FROM sessions WHERE id=?`, id,
	).Scan(&s.ID, &s.UserID, &s.ExpiresAt, &s.CreatedAt, &s.UserAgent, &s.IP)
	if err == sql.ErrNoRows {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	return s, nil
}

// DeleteSession removes the session with the given ID.
func (x *DB) DeleteSession(ctx context.Context, id string) error {
	_, err := x.sqlDB.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteExpiredSessions removes all sessions whose expires_at is in the past
// and returns the number of rows deleted.
func (x *DB) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	res, err := x.sqlDB.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at <= ?`, time.Now(),
	)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions rows affected: %w", err)
	}
	return n, nil
}
