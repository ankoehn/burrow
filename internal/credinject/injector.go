package credinject

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
)

// Binding is the per-service upstream-credential binding row (mirrors the
// service_upstream_credentials table). The Store interface reads/writes these.
type Binding struct {
	ServiceID    string
	Slot         string
	HeaderName   string
	HeaderFormat string // must contain the literal "{key}" template variable
}

// Store is the durable binding surface. The *db.DB implementation in
// internal/db/upstream_credentials.go satisfies it; tests provide stubs.
type Store interface {
	// GetBinding returns the binding for serviceID, or (Binding{}, false, nil)
	// when no row exists. Non-nil error signals a database failure.
	GetBinding(ctx context.Context, serviceID string) (Binding, bool, error)
	// PutBinding upserts the binding row.
	PutBinding(ctx context.Context, b Binding) error
	// DeleteBinding removes the binding row. No error when no row existed.
	DeleteBinding(ctx context.Context, serviceID string) error
}

// Injector applies upstream credentials to outgoing proxy requests (spec B.3).
// It is a singleton constructed once at startup.
type Injector struct {
	v   Vault
	s   Store
	log *slog.Logger

	// OnInject is fired on every successful injection with (serviceID, slot).
	// Task 12 wires this to burrow_ai_credential_injections_total{service,slot}.
	OnInject func(serviceID, slot string)

	// OnMiss is called when a binding exists but the slot env var is absent.
	// The hook receives the serviceID. Task 12 wires this to
	// burrow_ai_credential_misses_total{service}.
	OnMiss func(serviceID string)
}

// New returns a new Injector. v and s must be non-nil.
func New(v Vault, s Store, log *slog.Logger) *Injector {
	return &Injector{
		v:      v,
		s:      s,
		log:    log,
		OnMiss: func(string) {}, // no-op default
	}
}

// Apply injects the upstream credential into r.Header in place (spec B.3):
//
//  1. Looks up the binding from the Store.
//  2. If unbound → returns (false, nil) — request passes through unchanged.
//  3. If the slot env var is absent → fires OnMiss, logs a warning, returns
//     (false, nil) — request passes through. The missing env var is a
//     configuration error, not a request error.
//  4. Strips any existing value for the configured header.
//  5. Sets the header to strings.Replace(HeaderFormat, "{key}", value, 1).
//  6. Returns (true, nil).
//
// Apply never logs credential values.
func (i *Injector) Apply(ctx context.Context, serviceID string, r *http.Request) (bool, error) {
	bind, ok, err := i.s.GetBinding(ctx, serviceID)
	if err != nil {
		return false, err
	}
	if !ok {
		// No binding configured for this service — pass-through.
		return false, nil
	}

	val, present := i.v.Get(bind.Slot)
	if !present {
		// Binding exists but env var is gone — misconfiguration.
		if i.OnMiss != nil {
			i.OnMiss(serviceID)
		}
		i.log.Warn("upstream credential slot missing from vault",
			"service_id", serviceID,
			"slot", bind.Slot,
		)
		return false, nil
	}

	// Strip the visitor-supplied header value, then inject the real credential.
	r.Header.Del(bind.HeaderName)
	r.Header.Set(bind.HeaderName, strings.Replace(bind.HeaderFormat, "{key}", val, 1))
	if i.OnInject != nil {
		i.OnInject(serviceID, bind.Slot)
	}
	return true, nil
}
