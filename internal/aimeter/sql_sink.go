package aimeter

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/ankoehn/burrow/internal/db"
)

// SQLSink writes one usage_events row per recorded Sample. Errors are
// logged at warn level via the configured logger and swallowed (the proxy
// path must never fail on a metering write).
type SQLSink struct {
	DB  *db.DB
	Log *slog.Logger // optional; if nil, slog.Default() is used
}

// NewSQLSink constructs a SQLSink using slog.Default for diagnostics. Callers
// who want a scoped logger may set Log directly on the returned struct.
func NewSQLSink(d *db.DB) *SQLSink {
	return &SQLSink{DB: d, Log: slog.Default()}
}

// Record inserts one usage_events row. The id is generated via uuid.NewString
// (same scheme other v0.4.0 tables use — see internal/db/services.go and the
// services/api-keys path). The Ts column defaults to time.Now().UTC().
//
// Non-blocking semantics: any sqlite error is logged + swallowed. The caller
// (proxy hot path) treats the returned error as informational only. Callers
// may still propagate it in tests via errcheck patterns.
func (s *SQLSink) Record(ctx context.Context, sm Sample) error {
	if s == nil || s.DB == nil {
		return nil
	}
	log := s.Log
	if log == nil {
		log = slog.Default()
	}
	row := db.UsageEvent{
		ID:             uuid.NewString(),
		ServiceID:      sm.ServiceID,
		APIKeyID:       sm.APIKeyID,
		Ts:             time.Now().UTC(),
		Kind:           string(sm.Kind),
		TokensIn:       int64(sm.TokensIn),
		TokensOut:      int64(sm.TokensOut),
		BytesIn:        sm.BytesIn,
		BytesOut:       sm.BytesOut,
		Streamed:       sm.Streamed,
		CacheHit:       sm.CacheHit,
		UpstreamStatus: sm.UpstreamStatus,
	}
	_, err := s.DB.DB().ExecContext(ctx, `
		INSERT INTO usage_events
		  (id, service_id, api_key_id, ts, kind,
		   tokens_in, tokens_out, bytes_in, bytes_out,
		   streamed, cache_hit, upstream_status)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		row.ID, row.ServiceID, row.APIKeyID, row.Ts, row.Kind,
		row.TokensIn, row.TokensOut, row.BytesIn, row.BytesOut,
		boolToInt(row.Streamed), boolToInt(row.CacheHit), row.UpstreamStatus,
	)
	if err != nil {
		log.Warn("aimeter: usage_events insert failed",
			slog.String("service_id", sm.ServiceID),
			slog.String("api_key_id", sm.APIKeyID),
			slog.String("kind", string(sm.Kind)),
			slog.String("err", err.Error()),
		)
		// Swallow: non-blocking per v0.4.0 spec.
		return nil
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
