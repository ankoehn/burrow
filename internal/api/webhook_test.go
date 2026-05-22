package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/db"
)

// fakeWebhookStore is an in-memory WebhookStore for handler tests.
type fakeWebhookStore struct {
	mu         sync.Mutex
	webhooks   map[string]db.Webhook
	deliveries []db.WebhookDelivery
	// Allow tests to inject errors for paths the happy-path can't reach.
	listErr error
}

func newFakeWebhookStore() *fakeWebhookStore {
	return &fakeWebhookStore{webhooks: map[string]db.Webhook{}}
}

func (f *fakeWebhookStore) ListWebhooks(_ context.Context) ([]db.Webhook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]db.Webhook, 0, len(f.webhooks))
	for _, w := range f.webhooks {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (f *fakeWebhookStore) GetWebhook(_ context.Context, id string) (db.Webhook, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.webhooks[id]
	if !ok {
		return db.Webhook{}, db.ErrNotFound
	}
	return w, nil
}

func (f *fakeWebhookStore) CreateWebhook(_ context.Context, w db.Webhook) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if w.CreatedAt.IsZero() {
		w.CreatedAt = time.Now().UTC()
	}
	f.webhooks[w.ID] = w
	return nil
}

func (f *fakeWebhookStore) UpdateWebhook(_ context.Context, w db.Webhook) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, ok := f.webhooks[w.ID]
	if !ok {
		return db.ErrNotFound
	}
	cur.Name = w.Name
	cur.URL = w.URL
	cur.Events = w.Events
	cur.Paused = w.Paused
	f.webhooks[w.ID] = cur
	return nil
}

func (f *fakeWebhookStore) DeleteWebhook(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.webhooks[id]; !ok {
		return db.ErrNotFound
	}
	delete(f.webhooks, id)
	// Cascade delivery rows.
	kept := f.deliveries[:0]
	for _, d := range f.deliveries {
		if d.WebhookID != id {
			kept = append(kept, d)
		}
	}
	f.deliveries = kept
	return nil
}

func (f *fakeWebhookStore) SetWebhookPaused(_ context.Context, id string, paused bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.webhooks[id]
	if !ok {
		return db.ErrNotFound
	}
	w.Paused = paused
	f.webhooks[id] = w
	return nil
}

func (f *fakeWebhookStore) ListWebhookDeliveries(_ context.Context, q db.WebhookDeliveryQuery) ([]db.WebhookDelivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]db.WebhookDelivery, 0, len(f.deliveries))
	for _, d := range f.deliveries {
		if q.WebhookID != "" && d.WebhookID != q.WebhookID {
			continue
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ts.After(out[j].Ts) })
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// fakeWebhookDispatcher captures DeliverNow calls + can simulate a
// delivery by appending to the underlying store.
type fakeWebhookDispatcher struct {
	mu       sync.Mutex
	calls    []deliverCall
	delivery func(webhookID, event string) // optional side effect
	failErr  error
}

type deliverCall struct {
	WebhookID string
	Event     string
	Payload   any
}

func (f *fakeWebhookDispatcher) DeliverNow(_ context.Context, id, event string, payload any) (int, int, error) {
	f.mu.Lock()
	f.calls = append(f.calls, deliverCall{WebhookID: id, Event: event, Payload: payload})
	side := f.delivery
	err := f.failErr
	f.mu.Unlock()
	if side != nil {
		side(id, event)
	}
	if err != nil {
		return 0, 0, err
	}
	return 1, 200, nil
}

func (f *fakeWebhookDispatcher) Publish(_ context.Context, event string, payload any) {
	f.mu.Lock()
	f.calls = append(f.calls, deliverCall{Event: event, Payload: payload})
	f.mu.Unlock()
}

// fakeSecretRegistry records Set/Delete calls.
type fakeSecretRegistry struct {
	mu      sync.Mutex
	stored  map[string]string
	deleted []string
}

func newFakeSecretRegistry() *fakeSecretRegistry {
	return &fakeSecretRegistry{stored: map[string]string{}}
}

func (f *fakeSecretRegistry) Set(id, plaintext string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stored[id] = plaintext
}

func (f *fakeSecretRegistry) Delete(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.stored, id)
	f.deleted = append(f.deleted, id)
}

// newWebhookTestServer wires the minimum Deps for the webhook-handler
// suite. Tests can override fields on the returned struct before
// calling NewRouter — but newWebhookTestServer is the common shortcut
// when defaults suffice.
func newWebhookTestServer(t *testing.T, role string) (
	*httptest.Server, *authClient,
	*fakeWebhookStore, *fakeWebhookDispatcher, *fakeSecretRegistry,
) {
	t.Helper()
	store := newFakeWebhookStore()
	dsp := &fakeWebhookDispatcher{}
	secrets := newFakeSecretRegistry()
	d := Deps{
		Log:               discardLog(),
		Users:             &fakeUserStore{role: role},
		Webhooks:          store,
		WebhookDispatcher: dsp,
		WebhookSecrets:    secrets,
	}
	srv := httptest.NewServer(NewRouter(d))
	t.Cleanup(srv.Close)
	c := authedClient(t, srv)
	return srv, c, store, dsp, secrets
}

// ---------------------------------------------------------------------------
// CRUD round-trip
// ---------------------------------------------------------------------------

func TestWebhookHandler_GetEmpty(t *testing.T) {
	_, c, _, _, _ := newWebhookTestServer(t, "admin")
	r := c.get(t, "/api/v1/webhooks")
	if r.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	var out []webhookResp
	if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if out == nil || len(out) != 0 {
		t.Fatalf("empty list must be non-nil [], got %v", out)
	}
}

func TestWebhookHandler_PostReturnsSecretOnce(t *testing.T) {
	_, c, store, _, secrets := newWebhookTestServer(t, "admin")
	body := map[string]any{
		"name":   "alerts",
		"url":    "https://example.com/wh",
		"events": []string{"tunnel.connected", "tunnel.failed"},
	}
	r := c.post(t, "/api/v1/webhooks", body)
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("POST status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	var created webhookResp
	if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if created.ID == "" || created.Name != "alerts" || created.URL != "https://example.com/wh" {
		t.Fatalf("round-trip mismatch: %+v", created)
	}
	if len(created.Events) != 2 {
		t.Fatalf("events len = %d want 2", len(created.Events))
	}
	if created.SigningSecret == "" {
		t.Fatal("POST response must include signing_secret plaintext")
	}
	if !strings.HasPrefix(created.SigningSecret, "whsec_") {
		t.Errorf("signing_secret prefix: got %q", created.SigningSecret)
	}
	// Registry got the plaintext.
	if got := secrets.stored[created.ID]; got != created.SigningSecret {
		t.Errorf("registry plaintext mismatch: got %q want %q", got, created.SigningSecret)
	}
	// DB has the hashed form, not the plaintext.
	stored := store.webhooks[created.ID]
	if stored.SecretHash == "" || strings.Contains(stored.SecretHash, "whsec_") {
		t.Errorf("DB must store hash, not plaintext: %q", stored.SecretHash)
	}

	// GET must NOT echo the plaintext.
	r2 := c.get(t, "/api/v1/webhooks")
	if r2.StatusCode != http.StatusOK {
		t.Fatal(readBody(t, r2))
	}
	body2 := readBody(t, r2)
	if strings.Contains(body2, created.SigningSecret) {
		t.Fatalf("GET /webhooks leaked signing_secret in response body:\n%s", body2)
	}
	if strings.Contains(body2, `"signing_secret"`) {
		t.Fatalf("GET /webhooks must omit signing_secret key:\n%s", body2)
	}
}

func TestWebhookHandler_PostValidation(t *testing.T) {
	_, c, _, _, _ := newWebhookTestServer(t, "admin")
	cases := []struct {
		name string
		body map[string]any
	}{
		{"empty name", map[string]any{
			"name": "", "url": "https://x.com", "events": []string{"webhook.test"}}},
		{"empty url", map[string]any{
			"name": "n", "url": "", "events": []string{"webhook.test"}}},
		{"bad url scheme", map[string]any{
			"name": "n", "url": "ftp://x.com", "events": []string{"webhook.test"}}},
		{"no events", map[string]any{
			"name": "n", "url": "https://x.com", "events": []string{}}},
		{"unknown event", map[string]any{
			"name": "n", "url": "https://x.com", "events": []string{"not.a.real.event"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := c.post(t, "/api/v1/webhooks", tc.body)
			if r.StatusCode != http.StatusBadRequest {
				t.Fatalf("status=%d want 400 body=%s", r.StatusCode, readBody(t, r))
			}
			r.Body.Close()
		})
	}
}

func TestWebhookHandler_PutUpdates(t *testing.T) {
	_, c, store, _, _ := newWebhookTestServer(t, "admin")
	// Pre-seed.
	store.webhooks["wh1"] = db.Webhook{
		ID: "wh1", Name: "old", URL: "https://example.com/old",
		SecretHash: "h", Events: `["webhook.test"]`, CreatedAt: time.Now(),
	}
	r := c.put(t, "/api/v1/webhooks/wh1", map[string]any{
		"name":   "new",
		"url":    "https://example.com/new",
		"events": []string{"webhook.test", "tunnel.failed"},
		"paused": true,
	})
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	r.Body.Close()
	got := store.webhooks["wh1"]
	if got.Name != "new" || got.URL != "https://example.com/new" || !got.Paused {
		t.Fatalf("update missed: %+v", got)
	}
	if got.SecretHash != "h" {
		t.Fatalf("Update must NOT touch secret_hash; got %q", got.SecretHash)
	}
	// 404 on missing.
	r2 := c.put(t, "/api/v1/webhooks/missing", map[string]any{
		"name": "x", "url": "https://x.com", "events": []string{"webhook.test"}})
	if r2.StatusCode != http.StatusNotFound {
		t.Fatalf("missing PUT status=%d want 404", r2.StatusCode)
	}
	r2.Body.Close()
}

func TestWebhookHandler_Delete(t *testing.T) {
	_, c, store, _, secrets := newWebhookTestServer(t, "admin")
	store.webhooks["wh1"] = db.Webhook{
		ID: "wh1", Name: "x", URL: "https://example.com",
		SecretHash: "h", Events: `["webhook.test"]`, CreatedAt: time.Now(),
	}
	secrets.stored["wh1"] = "whsec_xyz"
	r := c.delete(t, "/api/v1/webhooks/wh1")
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("status=%d", r.StatusCode)
	}
	r.Body.Close()
	if _, ok := store.webhooks["wh1"]; ok {
		t.Fatal("row not deleted")
	}
	if _, ok := secrets.stored["wh1"]; ok {
		t.Fatal("secret registry not cleaned up on delete")
	}
	// 404 on second delete.
	r2 := c.delete(t, "/api/v1/webhooks/wh1")
	if r2.StatusCode != http.StatusNotFound {
		t.Fatalf("second delete status=%d want 404", r2.StatusCode)
	}
	r2.Body.Close()
}

func TestWebhookHandler_PauseResume(t *testing.T) {
	_, c, store, _, _ := newWebhookTestServer(t, "admin")
	store.webhooks["wh1"] = db.Webhook{
		ID: "wh1", Name: "x", URL: "https://example.com",
		SecretHash: "h", Events: `["webhook.test"]`, CreatedAt: time.Now(),
	}
	r := c.post(t, "/api/v1/webhooks/wh1/pause", nil)
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("pause status=%d", r.StatusCode)
	}
	r.Body.Close()
	if !store.webhooks["wh1"].Paused {
		t.Fatal("pause did not stick")
	}
	r = c.post(t, "/api/v1/webhooks/wh1/resume", nil)
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("resume status=%d", r.StatusCode)
	}
	r.Body.Close()
	if store.webhooks["wh1"].Paused {
		t.Fatal("resume did not stick")
	}
}

// ---------------------------------------------------------------------------
// /test endpoint
// ---------------------------------------------------------------------------

func TestWebhookHandler_PostTest_DeliversAndRecords(t *testing.T) {
	_, c, store, dsp, _ := newWebhookTestServer(t, "admin")
	store.webhooks["wh1"] = db.Webhook{
		ID: "wh1", Name: "x", URL: "https://example.com",
		SecretHash: "h", Events: `["webhook.test"]`, CreatedAt: time.Now(),
	}
	// Have the dispatcher record one delivery row as a side effect, so
	// we can assert the handler "captures" it (per spec invariant).
	dsp.delivery = func(id, event string) {
		store.mu.Lock()
		defer store.mu.Unlock()
		store.deliveries = append(store.deliveries, db.WebhookDelivery{
			ID: "d1", WebhookID: id, Event: event, Ts: time.Now(),
			StatusCode: 200, Attempt: 1, LatencyMs: 10,
		})
	}
	r := c.post(t, "/api/v1/webhooks/wh1/test", nil)
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	r.Body.Close()
	if len(dsp.calls) != 1 || dsp.calls[0].Event != "webhook.test" || dsp.calls[0].WebhookID != "wh1" {
		t.Fatalf("dispatcher calls: %+v", dsp.calls)
	}
	// One delivery row was recorded by the (fake) dispatcher.
	if len(store.deliveries) != 1 || store.deliveries[0].Event != "webhook.test" {
		t.Fatalf("delivery rows: %+v", store.deliveries)
	}
}

func TestWebhookHandler_PostTest_404OnMissing(t *testing.T) {
	_, c, _, dsp, _ := newWebhookTestServer(t, "admin")
	r := c.post(t, "/api/v1/webhooks/missing/test", nil)
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d want 404", r.StatusCode)
	}
	r.Body.Close()
	if len(dsp.calls) != 0 {
		t.Fatal("dispatcher must not be called on missing webhook")
	}
}

// ---------------------------------------------------------------------------
// /deliveries endpoint
// ---------------------------------------------------------------------------

func TestWebhookHandler_GetDeliveries(t *testing.T) {
	_, c, store, _, _ := newWebhookTestServer(t, "admin")
	now := time.Now().UTC()
	store.deliveries = []db.WebhookDelivery{
		{ID: "a", WebhookID: "wh1", Event: "tunnel.connected",
			Ts: now.Add(-2 * time.Minute), StatusCode: 200, Attempt: 1, LatencyMs: 10},
		{ID: "b", WebhookID: "wh1", Event: "tunnel.failed",
			Ts: now.Add(-1 * time.Minute), StatusCode: 500, Attempt: 1, LatencyMs: 99},
		{ID: "c", WebhookID: "wh2", Event: "webhook.test",
			Ts: now, StatusCode: 200, Attempt: 1, LatencyMs: 5},
	}
	// No filter: 3 rows, ts DESC.
	r := c.get(t, "/api/v1/webhooks/deliveries")
	if r.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", r.StatusCode)
	}
	var rows []deliveryResp
	if err := json.NewDecoder(r.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if len(rows) != 3 {
		t.Fatalf("len=%d want 3", len(rows))
	}
	if rows[0].ID != "c" {
		t.Errorf("ts DESC order: rows[0]=%s want c", rows[0].ID)
	}
	// webhook_id filter.
	r2 := c.get(t, "/api/v1/webhooks/deliveries?webhook_id=wh2")
	rows = nil
	json.NewDecoder(r2.Body).Decode(&rows)
	r2.Body.Close()
	if len(rows) != 1 || rows[0].WebhookID != "wh2" {
		t.Fatalf("filter: %+v", rows)
	}
	// limit cap.
	r3 := c.get(t, "/api/v1/webhooks/deliveries?limit=1")
	rows = nil
	json.NewDecoder(r3.Body).Decode(&rows)
	r3.Body.Close()
	if len(rows) != 1 {
		t.Fatalf("limit=1: got %d", len(rows))
	}
}

// ---------------------------------------------------------------------------
// Permission gating
// ---------------------------------------------------------------------------

func TestWebhookHandler_NonAdmin_NoPerm_Forbidden(t *testing.T) {
	// role "user" does not hold webhooks:manage.
	_, c, _, _, _ := newWebhookTestServer(t, "user")
	cases := []struct {
		method, path string
		body         map[string]any
	}{
		{http.MethodGet, "/api/v1/webhooks", nil},
		{http.MethodPost, "/api/v1/webhooks", map[string]any{
			"name": "x", "url": "https://x.com", "events": []string{"webhook.test"}}},
		{http.MethodPut, "/api/v1/webhooks/wh1", map[string]any{
			"name": "x", "url": "https://x.com", "events": []string{"webhook.test"}}},
		{http.MethodDelete, "/api/v1/webhooks/wh1", nil},
		{http.MethodPost, "/api/v1/webhooks/wh1/test", nil},
		{http.MethodPost, "/api/v1/webhooks/wh1/pause", nil},
		{http.MethodPost, "/api/v1/webhooks/wh1/resume", nil},
		{http.MethodGet, "/api/v1/webhooks/deliveries", nil},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			r := c.do(t, tc.method, tc.path, tc.body)
			if r.StatusCode != http.StatusForbidden {
				t.Fatalf("status=%d want 403 body=%s", r.StatusCode, readBody(t, r))
			}
			r.Body.Close()
		})
	}
}

// ---------------------------------------------------------------------------
// Nil-deps degrade gracefully
// ---------------------------------------------------------------------------

func TestWebhookHandler_NilStore_Degrades(t *testing.T) {
	d := Deps{
		Log:   discardLog(),
		Users: &fakeUserStore{role: "admin"},
		// Webhooks intentionally nil.
	}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)
	r := c.get(t, "/api/v1/webhooks")
	if r.StatusCode != http.StatusOK {
		t.Fatalf("nil store GET status=%d want 200", r.StatusCode)
	}
	body := readBody(t, r)
	if strings.TrimSpace(body) != "[]" {
		t.Errorf("nil store GET body=%q want []", body)
	}
	r = c.post(t, "/api/v1/webhooks", map[string]any{
		"name": "x", "url": "https://x.com", "events": []string{"webhook.test"}})
	if r.StatusCode != http.StatusInternalServerError {
		t.Fatalf("nil store POST status=%d want 500", r.StatusCode)
	}
	r.Body.Close()
}

// ---------------------------------------------------------------------------
// Smoke test: store error surfaces as 500
// ---------------------------------------------------------------------------

func TestWebhookHandler_GetListError(t *testing.T) {
	_, c, store, _, _ := newWebhookTestServer(t, "admin")
	store.listErr = errors.New("boom")
	r := c.get(t, "/api/v1/webhooks")
	if r.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d want 500", r.StatusCode)
	}
	r.Body.Close()
}
