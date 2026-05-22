package credinject_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ankoehn/burrow/internal/credinject"
)

func TestEnvVaultScansBurrowUpstreamKeyPrefix(t *testing.T) {
	t.Setenv("BURROW_UPSTREAM_KEY_OPENAI", "sk-test-1")
	t.Setenv("BURROW_UPSTREAM_KEY_X", "...")
	t.Setenv("BURROW_OTHER", "ignored")
	v := credinject.NewEnvVault()
	if got, ok := v.Get("OPENAI"); !ok || got != "sk-test-1" {
		t.Errorf("got (%q,%v); want (sk-test-1,true)", got, ok)
	}
	if _, ok := v.Get("OTHER"); ok {
		t.Error("must not pick up non-prefixed env")
	}
	if !slices.Equal(v.Slots(), []string{"OPENAI", "X"}) {
		t.Errorf("slots: %v", v.Slots())
	}
}

func TestEnvVaultFileVariantOverridesLiteral(t *testing.T) {
	// Write a temp file with the file-secret value.
	dir := t.TempDir()
	secretFile := filepath.Join(dir, "openai.txt")
	if err := os.WriteFile(secretFile, []byte("sk-from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("BURROW_UPSTREAM_KEY_OPENAI", "sk-literal")
	t.Setenv("BURROW_UPSTREAM_KEY_OPENAI_FILE", secretFile)
	v := credinject.NewEnvVault()
	got, ok := v.Get("OPENAI")
	if !ok {
		t.Fatal("OPENAI slot should be present")
	}
	if got != "sk-from-file" {
		t.Errorf("got %q; want sk-from-file (_FILE must win over literal)", got)
	}
}

func TestEnvVaultFileVariantTrimsTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	secretFile := filepath.Join(dir, "key.txt")
	if err := os.WriteFile(secretFile, []byte("sk-trimmed\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BURROW_UPSTREAM_KEY_TRIMTEST_FILE", secretFile)
	v := credinject.NewEnvVault()
	got, ok := v.Get("TRIMTEST")
	if !ok {
		t.Fatal("TRIMTEST slot should be present")
	}
	if got != "sk-trimmed" {
		t.Errorf("got %q; want sk-trimmed (CRLF stripped)", got)
	}
}

func TestEnvVaultRejectsLowercaseSlots(t *testing.T) {
	t.Setenv("BURROW_UPSTREAM_KEY_lower", "should-not-appear")
	v := credinject.NewEnvVault()
	if _, ok := v.Get("lower"); ok {
		t.Error("lowercase slot must be rejected")
	}
	// Also make sure the slot doesn't appear in the list.
	for _, s := range v.Slots() {
		if strings.ToLower(s) == s {
			t.Errorf("lowercase slot %q appeared in Slots()", s)
		}
	}
}

func TestEnvVaultSortOrder(t *testing.T) {
	t.Setenv("BURROW_UPSTREAM_KEY_ZZZ", "z")
	t.Setenv("BURROW_UPSTREAM_KEY_AAA", "a")
	t.Setenv("BURROW_UPSTREAM_KEY_MMM", "m")
	v := credinject.NewEnvVault()
	got := v.Slots()
	// Should contain at least AAA, MMM, ZZZ in order.
	last := ""
	for _, s := range got {
		if s < last {
			t.Errorf("slots not sorted: %v", got)
			break
		}
		last = s
	}
}

func TestEnvVaultLogValueRedacts(t *testing.T) {
	t.Setenv("BURROW_UPSTREAM_KEY_SECRET", "super-secret-value")
	v := credinject.NewEnvVault()

	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	logger.Info("testing vault logging", "vault", v)
	logged := buf.String()
	if strings.Contains(logged, "super-secret-value") {
		t.Errorf("vault LogValue leaked secret into log: %s", logged)
	}
	if !strings.Contains(logged, "<vault redacted>") {
		t.Errorf("vault LogValue should contain '<vault redacted>'; got: %s", logged)
	}
}
