// v05_wiring.go — v0.5.0 component construction and adapters.
//
// Provides buildV05Stack which constructs every new v0.5.0 component and
// populates the v05Stack bundle that main.go threads into the API Deps and
// the aigw.Chain. Kept in a dedicated file to mirror the v04_wiring.go
// pattern and keep main.go readable.
package main

import (
	"context"
	"log/slog"

	"github.com/ankoehn/burrow/internal/api"
	"github.com/ankoehn/burrow/internal/cache/semantic"
	"github.com/ankoehn/burrow/internal/credinject"
	"github.com/ankoehn/burrow/internal/db"
	"github.com/ankoehn/burrow/internal/metrics"
	"github.com/ankoehn/burrow/internal/proxy/customdomain"
)

// v05Stack bundles every v0.5.0 component that main.go threads into the API
// Deps, the aigw.Chain, and the proxy TLS configuration.
type v05Stack struct {
	// SemanticCache is the vector-similarity tier (NoopCache in default build;
	// chromem-backed when -tags=semantic_cache).
	SemanticCache semantic.Cache

	// CredVault is the env-only upstream-credential vault.
	CredVault *credinject.EnvVault

	// CredInjector is the singleton injector wired into the aigw.Chain.
	// Its OnInject / OnMiss hooks fire the metrics recorder counters.
	CredInjector *credinject.Injector

	// CustomDomainStore is the per-domain cert cache used by the proxy
	// TLS listener's GetCertificate callback and the CustomDomainCache
	// invalidation surface.
	CustomDomainStore *customdomain.Store

	// ConnLogDB satisfies api.ConnectionLogStore for the JSON API.
	ConnLogDB api.ConnectionLogStore
}

// buildV05Stack constructs every v0.5.0 component in dependency order.
// It does NOT touch the aigw.Chain (chain fields are set by the caller in
// main.go after both stacks are built, to keep wiring explicit).
func buildV05Stack(
	_ context.Context,
	wrapped *db.DB,
	metricsRec *metrics.Recorder,
	log *slog.Logger,
) (*v05Stack, error) {
	// --- semantic cache (build-tag-gated; NoopCache in default build) ------
	semCache := semantic.New(wrapped, log)

	// --- credinject vault + injector ---------------------------------------
	vault := credinject.NewEnvVault()
	// credStoreAdapter bridges *db.DB's method names to credinject.Store.
	injector := credinject.New(vault, credStoreAdapter{wrapped}, log)

	// Wire metric hooks BEFORE the injector is handed to the chain.
	if metricsRec != nil {
		injector.OnInject = func(svc, slot string) {
			metricsRec.IncAICredentialInjections(svc, slot)
		}
		injector.OnMiss = func(svc string) {
			metricsRec.IncAICredentialMisses(svc)
		}
	}

	// --- custom-domain cert store -----------------------------------------
	// FuncDBStore bridges customdomain's DBStore interface to *db.DB without
	// an import cycle (customdomain never imports db).
	cdStore := customdomain.New(&customdomain.FuncDBStore{
		Fn: func(ctx context.Context, hostname string) (customdomain.DomainRow, bool, error) {
			row, err := wrapped.LookupCustomDomainByHostname(ctx, hostname)
			if err != nil {
				if err == db.ErrNotFound {
					return customdomain.DomainRow{}, false, nil
				}
				return customdomain.DomainRow{}, false, err
			}
			return customdomain.DomainRow{
				ID:        row.ID,
				ServiceID: row.ServiceID,
				Hostname:  row.Hostname,
				CertPEM:   row.CertPEM,
				KeyPEM:    row.KeyPEM,
				NotAfter:  row.NotAfter,
			}, true, nil
		},
	})

	// --- connection log DB adapter ----------------------------------------
	connLogDB := api.NewConnLogDBAdapter(wrapped)

	return &v05Stack{
		SemanticCache:     semCache,
		CredVault:         vault,
		CredInjector:      injector,
		CustomDomainStore: cdStore,
		ConnLogDB:         connLogDB,
	}, nil
}

// ---------------------------------------------------------------------------
// Adapters
// ---------------------------------------------------------------------------

// credStoreAdapter bridges *db.DB (which uses GetUpstreamCredential /
// UpsertUpstreamCredential / DeleteUpstreamCredential) to the
// credinject.Store interface (GetBinding / PutBinding / DeleteBinding).
// The field names differ only for historical naming consistency inside each
// package; the semantics are identical.
type credStoreAdapter struct{ x *db.DB }

func (a credStoreAdapter) GetBinding(ctx context.Context, serviceID string) (credinject.Binding, bool, error) {
	row, err := a.x.GetUpstreamCredential(ctx, serviceID)
	if err != nil {
		if err == db.ErrNotFound {
			return credinject.Binding{}, false, nil
		}
		return credinject.Binding{}, false, err
	}
	return credinject.Binding{
		ServiceID:    row.ServiceID,
		Slot:         row.Slot,
		HeaderName:   row.HeaderName,
		HeaderFormat: row.HeaderFormat,
	}, true, nil
}

func (a credStoreAdapter) PutBinding(ctx context.Context, b credinject.Binding) error {
	return a.x.UpsertUpstreamCredential(ctx, db.ServiceUpstreamCredential{
		ServiceID:    b.ServiceID,
		Slot:         b.Slot,
		HeaderName:   b.HeaderName,
		HeaderFormat: b.HeaderFormat,
	})
}

func (a credStoreAdapter) DeleteBinding(ctx context.Context, serviceID string) error {
	return a.x.DeleteUpstreamCredential(ctx, serviceID)
}

// noopSemanticEngine satisfies api.SemanticEngine with zero-value returns.
// Used in the default (non-semantic_cache) build so the global clear + stats
// routes return empty JSON without requiring the chromem-go import.
type noopSemanticEngine struct{}

func (noopSemanticEngine) ClearAll(_ context.Context) error { return nil }
func (noopSemanticEngine) AggregateStats(_ context.Context) (api.SemanticStats, error) {
	return api.SemanticStats{}, nil
}
