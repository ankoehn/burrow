package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ankoehn/burrow/internal/audit"
	"github.com/ankoehn/burrow/internal/db"
)

// seedAuditDB opens a fresh sqlite at path, migrates, appends n
// audit_events through audit.Logger using a freshly-generated signing key,
// and returns the absolute file path. The key is persisted to the
// settings table so a subsequent verify call loads the same key.
func seedAuditDB(t *testing.T, n int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "verify.db")
	sqldb, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(sqldb); err != nil {
		t.Fatal(err)
	}
	x := db.Wrap(sqldb)
	// Persist a deterministic ed25519 key under audit.signing_key so
	// the CLI's LoadOrGenerateSigningKey path picks it up rather than
	// generating a fresh one.
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ss := &settingsAdapter{x: x}
	if err := ss.SaveSettings(context.Background(), map[string]string{
		audit.SettingsKey: encodeSigningKey(priv),
	}); err != nil {
		t.Fatal(err)
	}
	l := audit.NewLogger(x, priv, nil)
	for i := 0; i < n; i++ {
		if err := l.Append(context.Background(), audit.Event{
			ActorID: "u-actor", ActorEmail: "admin@x", Action: audit.ActionUserCreate,
			SubjectID: "u-sub", SubjectLabel: "new@x", Result: "ok",
			Payload: json.RawMessage(`{"role":"user"}`),
		}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	_ = sqldb.Close()
	return path
}

// settingsAdapter is a minimal *db.DB → audit.SettingsStore bridge for the
// CLI test seed. The real CLI uses *store.Store (which exposes the same
// two methods); we don't need to import all of store here.
type settingsAdapter struct{ x *db.DB }

func (s *settingsAdapter) GetSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.x.GetAllSettings(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.Key] = r.Value
	}
	return out, nil
}
func (s *settingsAdapter) SaveSettings(ctx context.Context, kv map[string]string) error {
	return s.x.SetSettings(ctx, kv)
}

// encodeSigningKey mirrors what LoadOrGenerateSigningKey writes on first
// boot (std base64 of the 64-byte ed25519 private key) so a seed run can
// pre-plant the key without driving NewLogger first.
func encodeSigningKey(priv ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(priv)
}

// TestAuditVerifyCLI_OK seeds 3 events and asserts the CLI exits 0 with
// the expected stdout message.
func TestAuditVerifyCLI_OK(t *testing.T) {
	dbPath := seedAuditDB(t, 3)
	cmd := newAuditVerifyCmd()
	cmd.SetArgs([]string{"--db", dbPath})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v (stdout=%q stderr=%q)", err, stdout.String(), stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "Chain valid from ") || !strings.HasSuffix(strings.TrimSpace(stdout.String()), ".") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

// TestAuditVerifyCLI_Tamper seeds 3 events, tampers row 2's payload, and
// asserts the CLI returns the sentinel error (exit 1) and writes the
// mismatch line to stderr.
func TestAuditVerifyCLI_Tamper(t *testing.T) {
	dbPath := seedAuditDB(t, 3)
	// Reopen and tamper the middle row.
	sqldb, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	x := db.Wrap(sqldb)
	rows, _ := x.ListAuditEvents(context.Background(), db.AuditQuery{Limit: 100})
	// id DESC: rows[1] is the chronological second-newest. (3 rows total.)
	target := rows[1].ID
	if err := x.TamperAuditPayload(context.Background(), target, `{"role":"admin"}`); err != nil {
		t.Fatal(err)
	}
	_ = sqldb.Close()

	cmd := newAuditVerifyCmd()
	cmd.SetArgs([]string{"--db", dbPath})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	err = cmd.Execute()
	if err == nil {
		t.Fatalf("expected non-nil error (exit 1), got nil (stdout=%q)", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Chain mismatch at "+target+".") {
		t.Fatalf("unexpected stderr: %q (target=%s)", stderr.String(), target)
	}
}

// TestAuditVerifyCLI_MissingDBExits1 asserts a non-existent --db path
// fails cleanly with exit 1 and an error on stderr.
func TestAuditVerifyCLI_MissingDBExits1(t *testing.T) {
	cmd := newAuditVerifyCmd()
	cmd.SetArgs([]string{"--db", filepath.Join(t.TempDir(), "does-not-exist.db")})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(stderr.String(), "error:") {
		t.Fatalf("expected error message in stderr, got %q", stderr.String())
	}
}
