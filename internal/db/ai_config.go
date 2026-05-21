package db

// ai_config.go — raw read of service_ai_config rows. Decoding into the
// typed aigw.ServiceAIConfig lives in cmd/server (the chainConfigLoader
// adapter) because internal/cache/exact (the cache.Settings sub-type)
// already imports internal/db; pulling aigw types in here would close an
// import cycle. The cmd/server layer already imports both packages so
// the decode is wired there without violating dependency direction.
//
// Fail-open contract: callers treat any non-nil error as ok=false +
// fall through to v0.3.0 pass-through, so a malformed config row can
// never break the proxy.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// GetServiceAIConfigRaw returns the raw JSON blob persisted under
// service_ai_config.config for the given service id. ok=false (with a
// nil error) when no row exists — preserves the v0.3.0 pass-through
// invariant for services that have not opted into AI features.
//
// The caller (cmd/server's chainConfigLoader) decodes the blob into
// aigw.ServiceAIConfig. See the package doc comment for the reason the
// decode is not done here.
func (x *DB) GetServiceAIConfigRaw(ctx context.Context, serviceID string) (raw []byte, ok bool, err error) {
	var s sql.NullString
	err = x.sqlDB.QueryRowContext(ctx,
		`SELECT config FROM service_ai_config WHERE service_id=?`, serviceID,
	).Scan(&s)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get service ai config: %w", err)
	}
	if !s.Valid {
		return nil, true, nil
	}
	return []byte(s.String), true, nil
}
