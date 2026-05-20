package db

// webauthn.go — typed CRUD over the webauthn_credentials table (migration
// 0007). The id column is the base64url-encoded credential id (the
// authenticator's raw credentialId). public_key is the COSE-encoded public
// key blob; sign_count is the authenticator's last observed counter; aaguid
// and transports are nullable optional metadata.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// CreateWebAuthnCredential inserts a new webauthn_credentials row. created_at
// is populated by the SQLite default.
func (x *DB) CreateWebAuthnCredential(ctx context.Context, c WebAuthnCredential) error {
	var aaguid, transports interface{}
	if c.AAGUID != nil {
		aaguid = *c.AAGUID
	}
	if c.Transports != nil {
		transports = *c.Transports
	}
	_, err := x.sqlDB.ExecContext(ctx,
		`INSERT INTO webauthn_credentials(id, user_id, label, public_key, sign_count, aaguid, transports)
		 VALUES(?,?,?,?,?,?,?)`,
		c.ID, c.UserID, c.Label, c.PublicKey, c.SignCount, aaguid, transports,
	)
	if err != nil {
		return fmt.Errorf("create webauthn credential: %w", err)
	}
	return nil
}

// GetWebAuthnCredential returns the row with the given id, or ErrNotFound.
func (x *DB) GetWebAuthnCredential(ctx context.Context, id string) (WebAuthnCredential, error) {
	return x.scanWebAuthnCredential(x.sqlDB.QueryRowContext(ctx,
		`SELECT id, user_id, label, public_key, sign_count, aaguid, transports,
		        created_at, last_used
		   FROM webauthn_credentials WHERE id=?`, id))
}

// ListWebAuthnCredentialsByUser returns every credential owned by the given
// user, ordered by created_at ascending. The slice is always non-nil.
func (x *DB) ListWebAuthnCredentialsByUser(ctx context.Context, userID string) ([]WebAuthnCredential, error) {
	rows, err := x.sqlDB.QueryContext(ctx,
		`SELECT id, user_id, label, public_key, sign_count, aaguid, transports,
		        created_at, last_used
		   FROM webauthn_credentials WHERE user_id=? ORDER BY created_at`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list webauthn credentials by user: %w", err)
	}
	defer rows.Close()
	return scanWebAuthnCredentials(rows)
}

// DeleteWebAuthnCredential removes the row with the given id. Returns
// ErrNotFound if no row matched.
func (x *DB) DeleteWebAuthnCredential(ctx context.Context, id string) error {
	res, err := x.sqlDB.ExecContext(ctx,
		`DELETE FROM webauthn_credentials WHERE id=?`, id,
	)
	if err != nil {
		return fmt.Errorf("delete webauthn credential: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete webauthn credential rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("delete webauthn credential: %w", ErrNotFound)
	}
	return nil
}

// UpdateWebAuthnSignCount sets the authenticator counter for the given
// credential id. Called after a successful login to keep the counter in step
// with the authenticator (replay defence). Returns ErrNotFound if no row
// matched.
func (x *DB) UpdateWebAuthnSignCount(ctx context.Context, id string, signCount int64) error {
	res, err := x.sqlDB.ExecContext(ctx,
		`UPDATE webauthn_credentials SET sign_count=?, last_used=CURRENT_TIMESTAMP WHERE id=?`,
		signCount, id,
	)
	if err != nil {
		return fmt.Errorf("update webauthn sign_count: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update webauthn sign_count rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("update webauthn sign_count: %w", ErrNotFound)
	}
	return nil
}

func (x *DB) scanWebAuthnCredential(row *sql.Row) (WebAuthnCredential, error) {
	var c WebAuthnCredential
	var aaguid, transports sql.NullString
	var lastUsed sql.NullTime
	err := row.Scan(&c.ID, &c.UserID, &c.Label, &c.PublicKey, &c.SignCount,
		&aaguid, &transports, &c.CreatedAt, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return WebAuthnCredential{}, ErrNotFound
	}
	if err != nil {
		return WebAuthnCredential{}, fmt.Errorf("get webauthn credential: %w", err)
	}
	if aaguid.Valid {
		s := aaguid.String
		c.AAGUID = &s
	}
	if transports.Valid {
		s := transports.String
		c.Transports = &s
	}
	if lastUsed.Valid {
		ts := lastUsed.Time.UTC()
		c.LastUsed = &ts
	}
	return c, nil
}

func scanWebAuthnCredentials(rows *sql.Rows) ([]WebAuthnCredential, error) {
	out := make([]WebAuthnCredential, 0)
	for rows.Next() {
		var c WebAuthnCredential
		var aaguid, transports sql.NullString
		var lastUsed sql.NullTime
		if err := rows.Scan(&c.ID, &c.UserID, &c.Label, &c.PublicKey, &c.SignCount,
			&aaguid, &transports, &c.CreatedAt, &lastUsed); err != nil {
			return nil, fmt.Errorf("scan webauthn credential: %w", err)
		}
		if aaguid.Valid {
			s := aaguid.String
			c.AAGUID = &s
		}
		if transports.Valid {
			s := transports.String
			c.Transports = &s
		}
		if lastUsed.Valid {
			ts := lastUsed.Time.UTC()
			c.LastUsed = &ts
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list webauthn credentials rows: %w", err)
	}
	return out, nil
}
