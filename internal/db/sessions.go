package db

import (
	"context"
	"database/sql"
	"fmt"
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
//
// modernc/sqlite v1.50.1 binds a Go time.Time via time.Time.String(), which
// produces text of the form "YYYY-MM-DD HH:MM:SS.fffffffff +0000 UTC" (space-
// separated, up to 9 fractional digits, " +0000 UTC" suffix). SQLite's
// datetime() / strftime() cannot parse this format directly — bare
// datetime(expires_at) would return ” — because of the " +0000 UTC" suffix.
//
// substr(expires_at, 1, 23) strips the unparseable suffix and keeps
// "YYYY-MM-DD HH:MM:SS.fff" (millisecond precision), which strftime can parse.
// Applying strftime('%Y-%m-%d %H:%M:%f', …) to BOTH sides of the comparison
// produces a true datetime (not lexical) comparison that is offset- and
// format-independent. No bound parameter is needed: strftime('now') returns
// the current UTC time directly inside SQLite.
func (x *DB) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	res, err := x.sqlDB.ExecContext(ctx,
		`DELETE FROM sessions
		 WHERE strftime('%Y-%m-%d %H:%M:%f', substr(expires_at, 1, 23))
		    <= strftime('%Y-%m-%d %H:%M:%f', 'now')`,
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
