package audit

import "context"

// ctxKey is the unexported key type for LogContext values stashed on a
// context.Context. Defined as an unexported empty struct so two callers in
// different packages cannot collide.
type ctxKey struct{}

var logContextKey ctxKey

// WithLogContext returns a new context carrying lc. HTTP middleware in
// internal/api derives lc from the authenticated session + request headers
// and threads it through to store methods so mutations can append typed
// audit events without re-parsing the request inside the store.
func WithLogContext(parent context.Context, lc LogContext) context.Context {
	return context.WithValue(parent, logContextKey, lc)
}

// LogContextFrom extracts the LogContext stashed by WithLogContext. Returns
// a zero value when none is present (the typed helpers all accept that
// zero value — actor_id/actor_email simply land empty, which is correct
// for CLI-driven and system-triggered audit rows).
func LogContextFrom(ctx context.Context) LogContext {
	if ctx == nil {
		return LogContext{}
	}
	v, _ := ctx.Value(logContextKey).(LogContext)
	return v
}
