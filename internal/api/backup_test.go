package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeBackupRunner emits a deterministic .tar.gz containing db.sqlite +
// manifest.json so the handler under test can be exercised without driving
// a real SQLite VACUUM INTO call.
type fakeBackupRunner struct {
	mu     sync.Mutex
	calls  int
	failed bool
}

func (f *fakeBackupRunner) RunBackup(_ context.Context, outPath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.failed {
		return fmt.Errorf("simulated failure")
	}
	return writeFakeArchive(outPath, "burrowd-test", []byte("FAKE DB BYTES"))
}

// fakeRestoreRunner records the staged archive path; the handler stages the
// uploaded file then hands it to RunRestore. We confirm the path lives
// inside the configured backup directory.
type fakeRestoreRunner struct {
	mu     sync.Mutex
	calls  int
	failed bool
	last   string
}

func (f *fakeRestoreRunner) RunRestore(_ context.Context, src string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.last = src
	if f.failed {
		return fmt.Errorf("simulated restore failure")
	}
	return nil
}

// writeFakeArchive emits a .tar.gz containing db.sqlite + manifest.json at
// outPath. The manifest's db_sha256 matches the bytes of db.sqlite so the
// verify endpoint reports a clean match. outPath MUST NOT exist already
// (the real CLI semantics are preserved).
func writeFakeArchive(outPath, version string, dbBytes []byte) error {
	f, err := os.OpenFile(outPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	if err := tw.WriteHeader(&tar.Header{
		Name: "db.sqlite", Mode: 0o600, Size: int64(len(dbBytes)),
		ModTime: time.Now().UTC(),
	}); err != nil {
		return err
	}
	if _, err := tw.Write(dbBytes); err != nil {
		return err
	}
	sum := sha256.Sum256(dbBytes)
	mb, _ := json.Marshal(map[string]any{
		"version":          version,
		"manifest_version": 1,
		"taken_at":         time.Now().UTC().Format(time.RFC3339Nano),
		"db_sha256":        hex.EncodeToString(sum[:]),
		"included":         []string{"db.sqlite", "manifest.json"},
	})
	if err := tw.WriteHeader(&tar.Header{
		Name: "manifest.json", Mode: 0o644, Size: int64(len(mb)),
		ModTime: time.Now().UTC(),
	}); err != nil {
		return err
	}
	if _, err := tw.Write(mb); err != nil {
		return err
	}
	return nil
}

// newBackupTestServer spins up a chi router with a fake backup runner +
// tracker and an admin-authed client.
func newBackupTestServer(t *testing.T, role string) (
	*httptest.Server, *authClient, string, *fakeBackupRunner, *fakeRestoreRunner,
) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	users := &fakeUserStore{role: role}
	br := &fakeBackupRunner{}
	rr := &fakeRestoreRunner{}
	tracker := NewRestoreTracker()
	d := Deps{
		Log:            discardLog(),
		Users:          users,
		BackupDir:      dir,
		BackupRunner:   br,
		RestoreRunner:  rr,
		RestoreTracker: tracker,
	}
	srv := httptest.NewServer(NewRouter(d))
	t.Cleanup(srv.Close)
	c := authedClient(t, srv)
	return srv, c, dir, br, rr
}

// TestBackupAPI_PostThenListEndToEnd covers Step 1 of the plan:
// POST /api/v1/backups returns 202 and the resulting archive shows up in
// GET /api/v1/backups.
func TestBackupAPI_PostThenListEndToEnd(t *testing.T) {
	_, c, _, br, _ := newBackupTestServer(t, "admin")

	r := c.post(t, "/api/v1/backups", map[string]any{})
	if r.StatusCode != http.StatusAccepted {
		t.Fatalf("POST status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	var started startBackupResp
	if err := json.NewDecoder(r.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if started.ID == "" {
		t.Fatalf("missing id")
	}
	if br.calls != 1 {
		t.Fatalf("RunBackup called %d times, want 1", br.calls)
	}

	r2 := c.get(t, "/api/v1/backups")
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", r2.StatusCode, readBody(t, r2))
	}
	var rows []backupRow
	if err := json.NewDecoder(r2.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if len(rows) != 1 {
		t.Fatalf("want 1 row got %d", len(rows))
	}
	if rows[0].ID != started.ID {
		t.Fatalf("id mismatch: list=%q post=%q", rows[0].ID, started.ID)
	}
	if rows[0].Version != "burrowd-test" {
		t.Fatalf("manifest version not surfaced: %q", rows[0].Version)
	}
	if rows[0].DBSha256 == "" {
		t.Fatalf("db_sha256 missing")
	}
}

// TestBackupAPI_NonAdminWithoutPermForbidden — a user without backup:run
// gets 403 on every backup route.
func TestBackupAPI_NonAdminWithoutPermForbidden(t *testing.T) {
	_, c, _, _, _ := newBackupTestServer(t, "user")
	r := c.get(t, "/api/v1/backups")
	if r.StatusCode != http.StatusForbidden {
		t.Fatalf("GET /backups for user: want 403 got %d body=%s",
			r.StatusCode, readBody(t, r))
	}
	r.Body.Close()
}

// TestBackupAPI_DownloadStreamsArchive — POST then GET /download returns
// the same bytes that landed on disk.
func TestBackupAPI_DownloadStreamsArchive(t *testing.T) {
	_, c, dir, _, _ := newBackupTestServer(t, "admin")
	r := c.post(t, "/api/v1/backups", map[string]any{})
	if r.StatusCode != http.StatusAccepted {
		t.Fatalf("POST status=%d", r.StatusCode)
	}
	var started startBackupResp
	json.NewDecoder(r.Body).Decode(&started)
	r.Body.Close()

	// Read the file directly and compare.
	want, err := os.ReadFile(filepath.Join(dir, started.ID+BackupArchiveExt))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	r2 := c.get(t, "/api/v1/backups/"+started.ID+"/download")
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("download status=%d body=%s", r2.StatusCode, readBody(t, r2))
	}
	defer r2.Body.Close()
	got, err := io.ReadAll(r2.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("download bytes differ (len got=%d want=%d)", len(got), len(want))
	}
	if ct := r2.Header.Get("Content-Type"); ct != "application/x-gzip" {
		t.Fatalf("Content-Type = %q", ct)
	}
}

// TestBackupAPI_VerifyMatch — verifies the integrity-anchor sha256.
func TestBackupAPI_VerifyMatch(t *testing.T) {
	_, c, _, _, _ := newBackupTestServer(t, "admin")
	r := c.post(t, "/api/v1/backups", map[string]any{})
	if r.StatusCode != http.StatusAccepted {
		t.Fatalf("POST status=%d", r.StatusCode)
	}
	var started startBackupResp
	json.NewDecoder(r.Body).Decode(&started)
	r.Body.Close()

	r2 := c.post(t, "/api/v1/backups/"+started.ID+"/verify", map[string]any{})
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", r2.StatusCode, readBody(t, r2))
	}
	var v verifyResp
	json.NewDecoder(r2.Body).Decode(&v)
	r2.Body.Close()
	if !v.OK || !v.Sha256Match {
		t.Fatalf("verify reported mismatch: %+v", v)
	}
}

// TestBackupAPI_Delete — DELETE removes the on-disk archive and returns 204.
func TestBackupAPI_Delete(t *testing.T) {
	_, c, dir, _, _ := newBackupTestServer(t, "admin")
	r := c.post(t, "/api/v1/backups", map[string]any{})
	var started startBackupResp
	json.NewDecoder(r.Body).Decode(&started)
	r.Body.Close()

	r2 := c.delete(t, "/api/v1/backups/"+started.ID)
	if r2.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status=%d body=%s", r2.StatusCode, readBody(t, r2))
	}
	r2.Body.Close()
	if _, err := os.Stat(filepath.Join(dir, started.ID+BackupArchiveExt)); !os.IsNotExist(err) {
		t.Fatalf("archive not removed: err=%v", err)
	}
}

// TestBackupAPI_Delete_BlocksTraversal — the id must not contain path
// separators or .. segments; an attacker cannot delete files outside the
// configured directory.
func TestBackupAPI_Delete_BlocksTraversal(t *testing.T) {
	_, c, _, _, _ := newBackupTestServer(t, "admin")
	// Use URL-escaped traversal so chi accepts it as an id segment.
	// chi splits on '/', so we can't pass a literal slash; instead try the
	// ".." segment which our handler refuses.
	r := c.delete(t, "/api/v1/backups/..")
	if r.StatusCode != http.StatusBadRequest && r.StatusCode != http.StatusNotFound {
		t.Fatalf("DELETE traversal: want 400/404 got %d body=%s",
			r.StatusCode, readBody(t, r))
	}
	r.Body.Close()
}

// TestBackupAPI_RestoreMultipartHappyPath — POST /backups/restore stages
// the uploaded archive and the tracker reports done. The runner is invoked
// with a path inside the configured backup directory.
func TestBackupAPI_RestoreMultipartHappyPath(t *testing.T) {
	srv, c, dir, _, rr := newBackupTestServer(t, "admin")

	// Build a valid archive in memory and POST it via multipart.
	archivePath := filepath.Join(t.TempDir(), "upload.tar.gz")
	if err := writeFakeArchive(archivePath, "burrowd-test", []byte("upload bytes")); err != nil {
		t.Fatal(err)
	}
	archiveBytes, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("archive", "upload.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(archiveBytes); err != nil {
		t.Fatal(err)
	}
	mw.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/backups/restore", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-CSRF-Token", c.csrf)
	resp, err := c.hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /restore status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	var r restoreStartResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if r.RestoreID == "" {
		t.Fatalf("missing restore_id")
	}

	// Poll the status endpoint until it leaves "running".
	deadline := time.Now().Add(2 * time.Second)
	var status restoreStatusResp
	for time.Now().Before(deadline) {
		sr := c.get(t, "/api/v1/backups/restores/"+r.RestoreID)
		if sr.StatusCode != http.StatusOK {
			t.Fatalf("status code=%d", sr.StatusCode)
		}
		json.NewDecoder(sr.Body).Decode(&status)
		sr.Body.Close()
		if status.Status != "running" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if status.Status != "done" {
		t.Fatalf("status = %q (err=%q)", status.Status, status.Error)
	}
	if rr.calls != 1 {
		t.Fatalf("runner calls=%d", rr.calls)
	}
	if !strings.HasPrefix(rr.last, dir) {
		t.Fatalf("runner src %q not inside backup dir %q", rr.last, dir)
	}
}

// TestBackupAPI_RestoreStatusNotFound — unknown restore_id returns 404.
func TestBackupAPI_RestoreStatusNotFound(t *testing.T) {
	_, c, _, _, _ := newBackupTestServer(t, "admin")
	r := c.get(t, "/api/v1/backups/restores/does-not-exist")
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d", r.StatusCode)
	}
	r.Body.Close()
}

// TestBackupAPI_PostBackupBubbleUpFailure — a failing runner surfaces 500.
func TestBackupAPI_PostBackupBubbleUpFailure(t *testing.T) {
	_, c, _, br, _ := newBackupTestServer(t, "admin")
	br.failed = true
	r := c.post(t, "/api/v1/backups", map[string]any{})
	if r.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", r.StatusCode, readBody(t, r))
	}
	r.Body.Close()
}
