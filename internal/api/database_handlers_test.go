package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRedactDatabaseURL verifies the URL redactor for both SQLite paths and
// Postgres DSNs. The password must never appear in the output.
func TestRedactDatabaseURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantOut string // substring that must be present
		noPass  string // substring that must NOT be present (the password)
	}{
		{
			name:    "sqlite file path unchanged",
			input:   "/var/lib/burrow/burrow.db",
			wantOut: "/var/lib/burrow/burrow.db",
		},
		{
			name:    "postgres with password",
			input:   "postgres://alice:s3cr3t@db.example.com/burrow?sslmode=verify-full",
			wantOut: "postgres://****:****@db.example.com/burrow?sslmode=verify-full",
			noPass:  "s3cr3t",
		},
		{
			name:    "postgres without credentials",
			input:   "postgres://db.example.com/burrow",
			wantOut: "postgres://db.example.com/burrow",
		},
		{
			name:    "empty string",
			input:   "",
			wantOut: "",
		},
		{
			name:    "relative sqlite path",
			input:   "./burrow.db",
			wantOut: "./burrow.db",
		},
		{
			name:    "postgres with only username no password",
			input:   "postgres://alice@db.example.com/burrow",
			wantOut: "db.example.com",
			noPass:  "alice",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactDatabaseURL(tc.input)
			if tc.wantOut != "" && !strings.Contains(got, tc.wantOut) {
				t.Errorf("RedactDatabaseURL(%q) = %q; want substring %q", tc.input, got, tc.wantOut)
			}
			if tc.noPass != "" && strings.Contains(got, tc.noPass) {
				t.Errorf("RedactDatabaseURL(%q) = %q; must NOT contain password %q", tc.input, got, tc.noPass)
			}
		})
	}
}

// TestGetDatabaseStatus_SQLite verifies the JSON shape returned for a SQLite
// deployment (the common default case).
func TestGetDatabaseStatus_SQLite(t *testing.T) {
	deps := Deps{
		Log: discardLog(),
		Database: DBInfo{
			Driver:      "sqlite",
			URLRedacted: "./burrow.db",
			Alpha:       false,
		},
	}
	// The route requires auth, but we can call the handler directly.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/database", nil)
	deps.GetDatabaseStatus(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := body["driver"]; got != "sqlite" {
		t.Errorf("driver = %v, want sqlite", got)
	}
	if got, _ := body["postgres_alpha"].(bool); got {
		t.Error("postgres_alpha must be false for SQLite")
	}
	if got := body["url_redacted"]; got != "./burrow.db" {
		t.Errorf("url_redacted = %v, want ./burrow.db", got)
	}
}

// TestGetDatabaseStatus_Postgres verifies the JSON shape returned for a
// Postgres deployment with the alpha flag set.
func TestGetDatabaseStatus_Postgres(t *testing.T) {
	deps := Deps{
		Log: discardLog(),
		Database: DBInfo{
			Driver:      "postgres",
			URLRedacted: "postgres://****:****@db.example.com/burrow?sslmode=verify-full",
			Alpha:       true,
		},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/database", nil)
	deps.GetDatabaseStatus(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := body["driver"]; got != "postgres" {
		t.Errorf("driver = %v, want postgres", got)
	}
	if got, _ := body["postgres_alpha"].(bool); !got {
		t.Error("postgres_alpha must be true")
	}
	redacted, _ := body["url_redacted"].(string)
	if strings.Contains(redacted, "s3cr3t") {
		t.Errorf("url_redacted must not contain plaintext password, got %q", redacted)
	}
	if !strings.Contains(redacted, "****") {
		t.Errorf("url_redacted should show **** placeholder, got %q", redacted)
	}
}
