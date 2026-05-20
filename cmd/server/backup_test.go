package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ankoehn/burrow/internal/db"
)

// seedRestorableDB creates a fresh SQLite database at path with the full
// burrow schema (so the audit logger can append a restore genesis row when
// the file is later restored).
func seedRestorableDB(t *testing.T, path string) {
	t.Helper()
	sqldb, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(sqldb); err != nil {
		t.Fatal(err)
	}
	if err := sqldb.Close(); err != nil {
		t.Fatal(err)
	}
}

// readArchive returns a map of tar-entry-name → bytes for a .tar.gz archive,
// used by tests to assert the wire contract.
func readArchive(t *testing.T, path string) map[string][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	out := map[string][]byte{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, tr); err != nil {
			t.Fatal(err)
		}
		out[hdr.Name] = buf.Bytes()
	}
	return out
}

// TestBackupCLI_WritesTarGzWithManifest covers Step 1 of the plan:
// `burrowd backup --to <tar.gz> --db <db>` produces a tar.gz with db.sqlite
// + manifest.json, and the manifest's db_sha256 matches sha256(db.sqlite).
func TestBackupCLI_WritesTarGzWithManifest(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "burrow.db")
	outPath := filepath.Join(tmp, "out.tar.gz")
	seedRestorableDB(t, dbPath)

	cmd := newBackupCmd()
	cmd.SetArgs([]string{"--to", outPath, "--db", dbPath})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetContext(context.Background())
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v (stderr=%q)", err, stderr.String())
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("output missing: %v", err)
	}

	entries := readArchive(t, outPath)
	dbBytes, ok := entries["db.sqlite"]
	if !ok {
		t.Fatalf("archive missing db.sqlite: keys=%v", keys(entries))
	}
	manifestBytes, ok := entries["manifest.json"]
	if !ok {
		t.Fatalf("archive missing manifest.json: keys=%v", keys(entries))
	}
	var manifest BackupManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("decode manifest: %v body=%s", err, manifestBytes)
	}
	if manifest.ManifestVersion != ManifestVersion {
		t.Fatalf("manifest_version = %d, want %d", manifest.ManifestVersion, ManifestVersion)
	}
	sum := sha256.Sum256(dbBytes)
	if got := hex.EncodeToString(sum[:]); got != manifest.DBSha256 {
		t.Fatalf("db_sha256 mismatch: archive=%s actual=%s", manifest.DBSha256, got)
	}
	for _, want := range []string{"db.sqlite", "manifest.json"} {
		found := false
		for _, inc := range manifest.Included {
			if inc == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("manifest.included missing %q: %v", want, manifest.Included)
		}
	}
}

// TestBackupCLI_RefusesExistingOutput asserts the CLI does not overwrite an
// existing archive. The operator must rename or delete the prior file.
func TestBackupCLI_RefusesExistingOutput(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "burrow.db")
	outPath := filepath.Join(tmp, "out.tar.gz")
	seedRestorableDB(t, dbPath)
	if err := os.WriteFile(outPath, []byte("squat"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newBackupCmd()
	cmd.SetArgs([]string{"--to", outPath, "--db", dbPath})
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetOut(io.Discard)
	cmd.SetContext(context.Background())
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected non-nil error (exit 1)")
	}
	if !strings.Contains(stderr.String(), "refusing to overwrite") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

// TestBackupCLI_MissingDBFails covers the explicit error path: the resolved
// --db path must exist on disk before VACUUM INTO runs.
func TestBackupCLI_MissingDBFails(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "does-not-exist.db")
	outPath := filepath.Join(tmp, "out.tar.gz")
	cmd := newBackupCmd()
	cmd.SetArgs([]string{"--to", outPath, "--db", dbPath})
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetOut(io.Discard)
	cmd.SetContext(context.Background())
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error for missing db")
	}
	if !strings.Contains(stderr.String(), "error:") {
		t.Fatalf("stderr missing error prefix: %q", stderr.String())
	}
}

// TestBackupCLI_IncludesOperatorTLSAndConfig covers the optional inclusions:
// when the backupOptions carry TLS cert + key + config file paths, the
// archive holds them under the spec-locked names.
func TestBackupCLI_IncludesOperatorTLSAndConfig(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "burrow.db")
	certPath := filepath.Join(tmp, "cert.pem")
	keyPath := filepath.Join(tmp, "key.pem")
	cfgPath := filepath.Join(tmp, "burrow.yaml")
	outPath := filepath.Join(tmp, "out.tar.gz")
	seedRestorableDB(t, dbPath)
	for _, p := range []struct{ path, body string }{
		{certPath, "-----BEGIN CERTIFICATE-----\nx\n-----END CERTIFICATE-----\n"},
		{keyPath, "-----BEGIN PRIVATE KEY-----\ny\n-----END PRIVATE KEY-----\n"},
		{cfgPath, "listen: :7000\n"},
	} {
		if err := os.WriteFile(p.path, []byte(p.body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := runBackup(context.Background(), backupOptions{
		DBPath:     dbPath,
		OutPath:    outPath,
		TLSCert:    certPath,
		TLSKey:     keyPath,
		ConfigFile: cfgPath,
	}, io.Discard); err != nil {
		t.Fatalf("runBackup: %v", err)
	}
	entries := readArchive(t, outPath)
	for _, want := range []string{"db.sqlite", "manifest.json", "tls/cert.pem", "tls/key.pem", "config/burrow.yaml"} {
		if _, ok := entries[want]; !ok {
			t.Fatalf("archive missing %q: keys=%v", want, keys(entries))
		}
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
