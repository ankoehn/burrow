package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ankoehn/burrow/internal/db"
	"github.com/ankoehn/burrow/internal/store"
)

// ---------------------------------------------------------------------------
// Helpers — tiny fakes wired together for these tests only
// ---------------------------------------------------------------------------

// newAIEndpointDeps assembles Deps for AI endpoint handler tests.
func newAIEndpointDeps(ss *fakeServiceStore, aliases *fakeModelAliasStore) Deps {
	return Deps{
		Users:        &fakeUserStore{role: "admin"},
		Services:     ss,
		ModelAliases: aliases,
		Log:          discardLog(),
	}
}

// newAIEndpointServer builds an httptest.Server with the given Deps and
// returns an authenticated authClient ready to call AI endpoint routes.
func newAIEndpointServer(t *testing.T, d Deps) (*httptest.Server, *authClient) {
	t.Helper()
	srv := httptest.NewServer(NewRouter(d))
	c := authedClient(t, srv)
	return srv, c
}

// seedAPIKey adds one fake API key entry to fakeServiceStore.listKeys so that
// api_key_count reflects at least one key for the given service.
func seedAPIKey(ss *fakeServiceStore, keyID string) {
	ss.listKeys = append(ss.listKeys, db.ServiceAPIKey{ID: keyID, Name: "k1"})
}

// ---------------------------------------------------------------------------
// TestAIEndpoints_ListReturnsEntryPerApiKeyService
// ---------------------------------------------------------------------------

func TestAIEndpoints_ListReturnsEntryPerApiKeyService(t *testing.T) {
	// Seed one api_key service and one open service — only api_key must appear.
	ss := &fakeServiceStore{
		listSvcs: []store.ServiceView{
			{ID: "svc-ai", Name: "my-llm", Type: "http", AccessMode: "api_key"},
			{ID: "svc-open", Name: "web", Type: "http", AccessMode: "open"},
		},
	}
	seedAPIKey(ss, "key-1")

	aliases := newFakeModelAliasStore()
	if err := aliases.CreateModelAlias(context.Background(), db.ModelAlias{
		Alias:         "gpt4o",
		ConcreteModel: "gpt-4o",
		ServiceID:     "svc-ai",
		Provider:      "openai",
		Priority:      100,
	}); err != nil {
		t.Fatalf("seed alias: %v", err)
	}

	srv, c := newAIEndpointServer(t, newAIEndpointDeps(ss, aliases))
	defer srv.Close()

	resp := c.get(t, "/api/v1/ai/endpoints")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	var out []aiEndpointResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()

	if len(out) != 1 {
		t.Fatalf("want 1 entry (api_key service only), got %d: %+v", len(out), out)
	}
	ep := out[0]

	if ep.ServiceID != "svc-ai" {
		t.Errorf("service_id: got %q want svc-ai", ep.ServiceID)
	}
	if ep.Name != "my-llm" {
		t.Errorf("name: got %q want my-llm", ep.Name)
	}
	if ep.ModelAlias != "gpt4o" {
		t.Errorf("model_alias: got %q want gpt4o", ep.ModelAlias)
	}
	if ep.ConcreteModel != "gpt-4o" {
		t.Errorf("concrete_model: got %q want gpt-4o", ep.ConcreteModel)
	}
	if ep.BackendType != "openai-compat" {
		t.Errorf("backend_type: got %q want openai-compat", ep.BackendType)
	}
	if ep.APIKeyCount != 1 {
		t.Errorf("api_key_count: got %d want 1", ep.APIKeyCount)
	}
	// Zeroed metering fields.
	if ep.Requests24h != 0 {
		t.Errorf("requests_24h: got %d want 0", ep.Requests24h)
	}
	if ep.CacheHits24h != 0 {
		t.Errorf("cache_hits_24h: got %d want 0", ep.CacheHits24h)
	}
	if ep.LatencyP95ms != 0 {
		t.Errorf("latency_p95_ms: got %d want 0", ep.LatencyP95ms)
	}
	// No live tunnel wired → Offline.
	if ep.Status != "Offline" {
		t.Errorf("status: got %q want Offline", ep.Status)
	}
}

// ---------------------------------------------------------------------------
// TestAIEndpointMetrics_ReturnsZeroedMetrics
// ---------------------------------------------------------------------------

func TestAIEndpointMetrics_ReturnsZeroedMetrics(t *testing.T) {
	ss := &fakeServiceStore{
		getSvc: store.ServiceDetail{
			ServiceView: store.ServiceView{ID: "svc-ai", Name: "my-llm", Type: "http", AccessMode: "api_key"},
		},
	}

	srv, c := newAIEndpointServer(t, newAIEndpointDeps(ss, newFakeModelAliasStore()))
	defer srv.Close()

	resp := c.get(t, "/api/v1/ai/endpoints/svc-ai/metrics")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	var out endpointMetricsResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()

	if out.Requests24h != 0 {
		t.Errorf("requests_24h: got %d want 0", out.Requests24h)
	}
	if out.TokensIn24h != 0 {
		t.Errorf("tokens_in_24h: got %d want 0", out.TokensIn24h)
	}
	if out.TokensOut24h != 0 {
		t.Errorf("tokens_out_24h: got %d want 0", out.TokensOut24h)
	}
	if out.CostUSD24h != 0 {
		t.Errorf("cost_usd_24h: got %v want 0", out.CostUSD24h)
	}
	if out.CacheHitRatio24h != 0 {
		t.Errorf("cache_hit_ratio_24h: got %v want 0", out.CacheHitRatio24h)
	}
	if len(out.RequestsPerMinute) != 60 {
		t.Errorf("requests_per_minute: got len=%d want 60", len(out.RequestsPerMinute))
	}
	for i, v := range out.RequestsPerMinute {
		if v != 0 {
			t.Errorf("requests_per_minute[%d]: got %d want 0", i, v)
		}
	}
}

// ---------------------------------------------------------------------------
// TestAIEndpointMetrics_404OnMissing
// ---------------------------------------------------------------------------

func TestAIEndpointMetrics_404OnMissing(t *testing.T) {
	ss := &fakeServiceStore{
		getSvcErr: db.ErrNotFound,
	}

	srv, c := newAIEndpointServer(t, newAIEndpointDeps(ss, newFakeModelAliasStore()))
	defer srv.Close()

	resp := c.get(t, "/api/v1/ai/endpoints/no-such-service/metrics")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", resp.StatusCode, readBody(t, resp))
	}
}
