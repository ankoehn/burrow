//go:build semantic_cache

package main

import (
	"context"

	"github.com/ankoehn/burrow/internal/api"
	"github.com/ankoehn/burrow/internal/cache/semantic"
	"github.com/ankoehn/burrow/internal/db"
)

// serviceLister is the narrow interface the semanticEngineAdapter uses to
// discover all registered service IDs. Implemented by serviceListerAdapter.
type serviceLister interface {
	GetAllServiceIDs(ctx context.Context) ([]string, error)
}

// serviceListerAdapter bridges *db.DB's ListAllServices to the serviceLister
// interface, extracting only the IDs. It avoids importing the full db.Service
// struct into the adapter layer.
type serviceListerAdapter struct{ x *db.DB }

func (a serviceListerAdapter) GetAllServiceIDs(ctx context.Context) ([]string, error) {
	rows, err := a.x.ListAllServices(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids, nil
}

// semanticEngineAdapter bridges a semantic.Cache (single-service operations)
// to the api.SemanticEngine interface (global clear + aggregate stats).
// It satisfies api.SemanticEngine by iterating all registered service IDs and
// delegating to the cache per service.
type semanticEngineAdapter struct {
	cache semantic.Cache
	svc   serviceLister // GetAllServiceIDs(ctx) ([]string, error)
}

// ClearAll removes every semantic index entry across all services by calling
// cache.ClearService for each registered service ID.
func (a semanticEngineAdapter) ClearAll(ctx context.Context) error {
	ids, err := a.svc.GetAllServiceIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := a.cache.ClearService(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// AggregateStats sums per-service Stats() across all registered services.
// Fields Entries, OnDiskBytes, SimilarReturned24h, and Promotions24h are
// summed directly. HitRate24h is averaged across services with at least one
// entry (a known approximation: per-service Stats() does not surface a
// request-count denominator, so a weighted average is not feasible). Services
// whose Stats() call fails are silently skipped (missing collection is
// non-fatal — the collection may not exist yet if no request was ever served).
func (a semanticEngineAdapter) AggregateStats(ctx context.Context) (api.SemanticStats, error) {
	ids, err := a.svc.GetAllServiceIDs(ctx)
	if err != nil {
		return api.SemanticStats{}, err
	}
	var out api.SemanticStats
	var hitRateCount int // number of services contributing to HitRate24h
	for _, id := range ids {
		s, err := a.cache.Stats(ctx, id)
		if err != nil {
			continue // non-fatal: no collection for this service yet
		}
		out.Entries += s.Entries
		out.OnDiskBytes += s.OnDiskBytes
		out.SimilarReturned24h += s.SimilarReturned24h
		out.Promotions24h += s.Promotions24h
		if s.Entries > 0 {
			// Include this service's hit-rate in the average only when it has
			// entries, so an empty service (HitRate24h == 0.0) doesn't dilute
			// the aggregate of active services.
			out.HitRate24h += s.HitRate24h
			hitRateCount++
		}
	}
	if hitRateCount > 1 {
		out.HitRate24h /= float64(hitRateCount)
	}
	return out, nil
}

// newSemanticEngine returns a semanticEngineAdapter backed by cache and
// a serviceListerAdapter over wrapped. Used in main.go (under semantic_cache
// build tag) to wire Deps.SemanticEngine with the production adapter.
func newSemanticEngine(cache semantic.Cache, wrapped *db.DB) api.SemanticEngine {
	return semanticEngineAdapter{
		cache: cache,
		svc:   serviceListerAdapter{x: wrapped},
	}
}
