package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

// selectServiceRow is the shared SELECT column list for service rows.
const selectServiceCols = `id, user_id, name, type, COALESCE(subdomain,''), access_mode, api_key_header, created_at`

// GetOrCreateService returns the service row for (userID, name), creating it
// with the given type and default access_mode/api_key_header if it does not
// exist. The operation is idempotent (INSERT ... ON CONFLICT DO NOTHING then
// SELECT), safe for concurrent callers.
func (x *DB) GetOrCreateService(ctx context.Context, userID, name, typ string) (Service, error) {
	id := uuid.NewString()
	_, err := x.sqlDB.ExecContext(ctx,
		`INSERT INTO services(id, user_id, name, type)
		 VALUES(?,?,?,?)
		 ON CONFLICT(user_id, name) DO NOTHING`,
		id, userID, name, typ,
	)
	if err != nil {
		return Service{}, fmt.Errorf("get or create service: %w", err)
	}
	var s Service
	err = x.sqlDB.QueryRowContext(ctx,
		`SELECT `+selectServiceCols+` FROM services WHERE user_id=? AND name=?`,
		userID, name,
	).Scan(&s.ID, &s.UserID, &s.Name, &s.Type, &s.Subdomain, &s.AccessMode, &s.APIKeyHeader, &s.CreatedAt)
	if err == sql.ErrNoRows {
		return Service{}, ErrNotFound
	}
	if err != nil {
		return Service{}, fmt.Errorf("get or create service select: %w", err)
	}
	return s, nil
}

// GetServiceByID returns the service with the given id, or ErrNotFound.
func (x *DB) GetServiceByID(ctx context.Context, id string) (Service, error) {
	var s Service
	err := x.sqlDB.QueryRowContext(ctx,
		`SELECT `+selectServiceCols+` FROM services WHERE id=?`, id,
	).Scan(&s.ID, &s.UserID, &s.Name, &s.Type, &s.Subdomain, &s.AccessMode, &s.APIKeyHeader, &s.CreatedAt)
	if err == sql.ErrNoRows {
		return Service{}, ErrNotFound
	}
	if err != nil {
		return Service{}, fmt.Errorf("get service by id: %w", err)
	}
	return s, nil
}

// GetServiceBySubdomain returns the service with the given subdomain, or
// ErrNotFound. Empty subdomain always returns ErrNotFound.
func (x *DB) GetServiceBySubdomain(ctx context.Context, sub string) (Service, error) {
	if sub == "" {
		return Service{}, ErrNotFound
	}
	var s Service
	err := x.sqlDB.QueryRowContext(ctx,
		`SELECT `+selectServiceCols+` FROM services WHERE subdomain=?`, sub,
	).Scan(&s.ID, &s.UserID, &s.Name, &s.Type, &s.Subdomain, &s.AccessMode, &s.APIKeyHeader, &s.CreatedAt)
	if err == sql.ErrNoRows {
		return Service{}, ErrNotFound
	}
	if err != nil {
		return Service{}, fmt.Errorf("get service by subdomain: %w", err)
	}
	return s, nil
}

// ListServicesByUser returns all service rows belonging to the given user,
// ordered by created_at.
func (x *DB) ListServicesByUser(ctx context.Context, userID string) ([]Service, error) {
	rows, err := x.sqlDB.QueryContext(ctx,
		`SELECT `+selectServiceCols+` FROM services WHERE user_id=? ORDER BY created_at`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list services by user: %w", err)
	}
	defer rows.Close()
	var out []Service
	for rows.Next() {
		var s Service
		if err := rows.Scan(&s.ID, &s.UserID, &s.Name, &s.Type, &s.Subdomain, &s.AccessMode, &s.APIKeyHeader, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan service: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list services by user rows: %w", err)
	}
	return out, nil
}

// ListAllServices returns all service rows ordered by created_at.
func (x *DB) ListAllServices(ctx context.Context) ([]Service, error) {
	rows, err := x.sqlDB.QueryContext(ctx,
		`SELECT `+selectServiceCols+` FROM services ORDER BY created_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("list all services: %w", err)
	}
	defer rows.Close()
	var out []Service
	for rows.Next() {
		var s Service
		if err := rows.Scan(&s.ID, &s.UserID, &s.Name, &s.Type, &s.Subdomain, &s.AccessMode, &s.APIKeyHeader, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan service: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list all services rows: %w", err)
	}
	return out, nil
}

// SetServiceAccessMode updates the access_mode and api_key_header for a
// service. Returns ErrNotFound if no row matched.
// Enum validation is the caller's responsibility (store layer).
func (x *DB) SetServiceAccessMode(ctx context.Context, id, mode, header string) error {
	res, err := x.sqlDB.ExecContext(ctx,
		`UPDATE services SET access_mode=?, api_key_header=? WHERE id=?`, mode, header, id,
	)
	if err != nil {
		return fmt.Errorf("set service access mode: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set service access mode rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetServiceSubdomain sets the subdomain for a service. Returns ErrNotFound if
// no row matched (service id does not exist). On a UNIQUE(subdomain) violation
// the raw driver error is wrapped so the caller (store layer) can detect a
// collision and retry with a different subdomain value.
func (x *DB) SetServiceSubdomain(ctx context.Context, id, sub string) error {
	res, err := x.sqlDB.ExecContext(ctx,
		`UPDATE services SET subdomain=? WHERE id=?`, sub, id,
	)
	if err != nil {
		// Wrap and surface; the UNIQUE constraint error from the sqlite driver is
		// preserved inside the wrapping so callers can inspect it via errors.As or
		// string-matching the driver error message.
		return fmt.Errorf("set service subdomain: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set service subdomain rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteService removes the service with the given ID.
// ON DELETE CASCADE removes associated api_keys and access_policy rows.
// Returns ErrNotFound if no row matched.
func (x *DB) DeleteService(ctx context.Context, id string) error {
	res, err := x.sqlDB.ExecContext(ctx, `DELETE FROM services WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete service rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("delete service: %w", ErrNotFound)
	}
	return nil
}

// CreateServiceAPIKey inserts a new service API key row.
func (x *DB) CreateServiceAPIKey(ctx context.Context, k ServiceAPIKey) error {
	_, err := x.sqlDB.ExecContext(ctx,
		`INSERT INTO service_api_keys(id, service_id, name, key_hash) VALUES(?,?,?,?)`,
		k.ID, k.ServiceID, k.Name, k.KeyHash,
	)
	if err != nil {
		return fmt.Errorf("create service api key: %w", err)
	}
	return nil
}

// GetServiceAPIKeyByHash returns the API key whose key_hash matches, scoped to
// the given serviceID. Returns ErrNotFound if no row matched.
func (x *DB) GetServiceAPIKeyByHash(ctx context.Context, serviceID, hash string) (ServiceAPIKey, error) {
	var k ServiceAPIKey
	var lastUsed sql.NullTime
	err := x.sqlDB.QueryRowContext(ctx,
		`SELECT id, service_id, name, key_hash, last_used, created_at
		   FROM service_api_keys WHERE service_id=? AND key_hash=?`,
		serviceID, hash,
	).Scan(&k.ID, &k.ServiceID, &k.Name, &k.KeyHash, &lastUsed, &k.CreatedAt)
	if err == sql.ErrNoRows {
		return ServiceAPIKey{}, ErrNotFound
	}
	if err != nil {
		return ServiceAPIKey{}, fmt.Errorf("get service api key by hash: %w", err)
	}
	if lastUsed.Valid {
		k.LastUsed = &lastUsed.Time
	}
	return k, nil
}

// ListServiceAPIKeys returns all API keys for a service, ordered by created_at.
func (x *DB) ListServiceAPIKeys(ctx context.Context, serviceID string) ([]ServiceAPIKey, error) {
	rows, err := x.sqlDB.QueryContext(ctx,
		`SELECT id, service_id, name, key_hash, last_used, created_at
		   FROM service_api_keys WHERE service_id=? ORDER BY created_at`,
		serviceID,
	)
	if err != nil {
		return nil, fmt.Errorf("list service api keys: %w", err)
	}
	defer rows.Close()
	var out []ServiceAPIKey
	for rows.Next() {
		var k ServiceAPIKey
		var lastUsed sql.NullTime
		if err := rows.Scan(&k.ID, &k.ServiceID, &k.Name, &k.KeyHash, &lastUsed, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan service api key: %w", err)
		}
		if lastUsed.Valid {
			k.LastUsed = &lastUsed.Time
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list service api keys rows: %w", err)
	}
	return out, nil
}

// TouchServiceAPIKey sets last_used to the current timestamp for the given key
// ID. Best-effort: exec errors are returned, but 0-rows-affected is a no-op.
func (x *DB) TouchServiceAPIKey(ctx context.Context, id string) error {
	_, err := x.sqlDB.ExecContext(ctx,
		`UPDATE service_api_keys SET last_used=CURRENT_TIMESTAMP WHERE id=?`, id,
	)
	if err != nil {
		return fmt.Errorf("touch service api key: %w", err)
	}
	return nil
}

// DeleteServiceAPIKey removes the API key with the given id, scoped to
// serviceID. Returns ErrNotFound if no row matched.
func (x *DB) DeleteServiceAPIKey(ctx context.Context, id, serviceID string) error {
	res, err := x.sqlDB.ExecContext(ctx,
		`DELETE FROM service_api_keys WHERE id=? AND service_id=?`, id, serviceID,
	)
	if err != nil {
		return fmt.Errorf("delete service api key: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete service api key rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("delete service api key: %w", ErrNotFound)
	}
	return nil
}

// GetAccessPolicy returns the roles for a service's access policy, ordered
// alphabetically. Always returns a non-nil slice (empty when no roles exist).
func (x *DB) GetAccessPolicy(ctx context.Context, serviceID string) ([]string, error) {
	rows, err := x.sqlDB.QueryContext(ctx,
		`SELECT role FROM service_access_policy WHERE service_id=? ORDER BY role`,
		serviceID,
	)
	if err != nil {
		return nil, fmt.Errorf("get access policy: %w", err)
	}
	defer rows.Close()
	out := []string{} // never nil — spec requires always-an-array
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, fmt.Errorf("scan access policy role: %w", err)
		}
		out = append(out, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get access policy rows: %w", err)
	}
	return out, nil
}

// SetAccessPolicy replaces the full access policy for a service in a single
// transaction: DELETE existing roles then INSERT the new set. An empty roles
// slice means deny-all (just the delete, no inserts). Mirrors the tx pattern
// in settings.go (SetSettings).
func (x *DB) SetAccessPolicy(ctx context.Context, serviceID string, roles []string) error {
	tx, err := x.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin access policy tx: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM service_access_policy WHERE service_id=?`, serviceID,
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete access policy: %w", err)
	}
	for _, role := range roles {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO service_access_policy(service_id, role) VALUES(?,?)`, serviceID, role,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert access policy role %q: %w", role, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit access policy tx: %w", err)
	}
	return nil
}
