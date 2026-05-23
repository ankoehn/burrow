package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeRetentionSettings implements SettingsStore for the retention endpoint
// tests.  GetSettings returns the seeded defaults from migration 0017.
type fakeRetentionSettings struct {
	saved map[string]string
}

func newFakeRetentionSettings() *fakeRetentionSettings {
	return &fakeRetentionSettings{}
}

func (f *fakeRetentionSettings) GetSettings(context.Context) (map[string]string, error) {
	return map[string]string{
		"audit.retention_days":                   "0",
		"inspector.retention_count":              "100",
		"usage.retention_days":                   "90",
		"redaction.retention_days":               "30",
		"connection_logs.retention_days":         "30",
		"connection_logs.rollups_retention_days": "0",
		"webhook_deliveries.retention_days":      "30",
	}, nil
}

func (f *fakeRetentionSettings) SaveSettings(_ context.Context, kv map[string]string) error {
	f.saved = kv
	return nil
}

func (f *fakeRetentionSettings) SendTestEmail(_ context.Context, _ string) error { return nil }

// TestGetSettingsRetentionReturnsAllSevenKeys verifies that GET
// /api/v1/settings/retention returns all seven retention keys plus the
// advisory note.
func TestGetSettingsRetentionReturnsAllSevenKeys(t *testing.T) {
	fs := newFakeRetentionSettings()
	d := Deps{Log: discardLog(), Settings: fs, Users: &fakeUserStore{role: "admin"}}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)

	r := c.get(t, "/api/v1/settings/retention")
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("GET /settings/retention status=%d body=%s", r.StatusCode, readBody(t, r))
	}

	var got map[string]any
	if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	wantKeys := []string{
		"audit_retention_days",
		"inspector_retention_count",
		"usage_retention_days",
		"redaction_retention_days",
		"connection_logs_retention_days",
		"connection_logs_rollups_retention_days",
		"webhook_deliveries_retention_days",
		"audit_retention_note",
	}
	for _, k := range wantKeys {
		if _, ok := got[k]; !ok {
			t.Errorf("response missing key %q; got keys: %v", k, keysOf(t, got))
		}
	}

	// Seeded defaults match migration 0017.
	if got["audit_retention_days"] != float64(0) {
		t.Errorf("audit_retention_days: want 0, got %v", got["audit_retention_days"])
	}
	if got["inspector_retention_count"] != float64(100) {
		t.Errorf("inspector_retention_count: want 100, got %v", got["inspector_retention_count"])
	}
	if got["usage_retention_days"] != float64(90) {
		t.Errorf("usage_retention_days: want 90, got %v", got["usage_retention_days"])
	}
	if got["audit_retention_note"] == "" {
		t.Error("audit_retention_note should be non-empty")
	}
}

// TestPutRetentionSettingsAcceptsValidUpdate verifies that a valid PUT request
// persists the provided fields.
func TestPutRetentionSettingsAcceptsValidUpdate(t *testing.T) {
	fs := newFakeRetentionSettings()
	d := Deps{Log: discardLog(), Settings: fs, Users: &fakeUserStore{role: "admin"}}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)

	body := map[string]any{
		"usage_retention_days":              180,
		"webhook_deliveries_retention_days": 60,
	}
	r := c.put(t, "/api/v1/settings/retention", body)
	defer r.Body.Close()
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT /settings/retention status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	if fs.saved["usage.retention_days"] != "180" {
		t.Errorf("saved usage.retention_days=%q want 180", fs.saved["usage.retention_days"])
	}
	if fs.saved["webhook_deliveries.retention_days"] != "60" {
		t.Errorf("saved webhook_deliveries.retention_days=%q want 60", fs.saved["webhook_deliveries.retention_days"])
	}
}

// TestPutRetentionSettingsValidatesRange verifies that out-of-range values
// return 400 Bad Request.
func TestPutRetentionSettingsValidatesRange(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
	}{
		{"usage negative", map[string]any{"usage_retention_days": -1}},
		{"audit too large", map[string]any{"audit_retention_days": 4000}},
		{"webhook_deliveries too large", map[string]any{"webhook_deliveries_retention_days": 400}},
		{"inspector_count zero", map[string]any{"inspector_retention_count": 0}},
		{"inspector_count too large", map[string]any{"inspector_retention_count": 1001}},
		{"connection_logs negative", map[string]any{"connection_logs_retention_days": -5}},
		{"rollups too large", map[string]any{"connection_logs_rollups_retention_days": 5000}},
		{"redaction zero", map[string]any{"redaction_retention_days": 0}},
	}

	fs := newFakeRetentionSettings()
	d := Deps{Log: discardLog(), Settings: fs, Users: &fakeUserStore{role: "admin"}}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := c.put(t, "/api/v1/settings/retention", tc.body)
			defer r.Body.Close()
			if r.StatusCode != http.StatusBadRequest {
				t.Errorf("%s: want 400 got %d body=%s", tc.name, r.StatusCode, readBody(t, r))
			}
		})
	}
}

// TestPutRetentionSettingsEmptyBodyIsNoOp verifies that an empty body (no
// fields) returns 204 without calling SaveSettings with any keys.
func TestPutRetentionSettingsEmptyBodyIsNoOp(t *testing.T) {
	fs := newFakeRetentionSettings()
	d := Deps{Log: discardLog(), Settings: fs, Users: &fakeUserStore{role: "admin"}}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)

	r := c.put(t, "/api/v1/settings/retention", map[string]any{})
	defer r.Body.Close()
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT empty body: want 204 got %d", r.StatusCode)
	}
	if fs.saved != nil {
		t.Errorf("SaveSettings should not have been called for an empty request; saved=%v", fs.saved)
	}
}

// TestRetentionSettingsAdminOnly verifies that non-admin users receive 403.
func TestRetentionSettingsAdminOnly(t *testing.T) {
	fs := newFakeRetentionSettings()
	d := Deps{Log: discardLog(), Settings: fs, Users: &fakeUserStore{role: "user"}}
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	c := authedClient(t, srv)

	r := c.get(t, "/api/v1/settings/retention")
	defer r.Body.Close()
	if r.StatusCode != http.StatusForbidden {
		t.Errorf("non-admin GET: want 403 got %d", r.StatusCode)
	}
}
