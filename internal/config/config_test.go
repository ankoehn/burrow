package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerDefaultListen(t *testing.T) {
	// No env set: assert the compiled-in default is :7000.
	c, err := LoadServer(nil)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if c.Listen != ":7000" {
		t.Fatalf("default listen = %q", c.Listen)
	}
}

func TestServerEnvOverrideListen(t *testing.T) {
	// Prove env-override works on a still-existing field.
	t.Setenv("BURROW_LISTEN", ":7777")
	c2, err := LoadServer(nil)
	if err != nil {
		t.Fatalf("LoadServer (env override): %v", err)
	}
	if c2.Listen != ":7777" {
		t.Fatalf("env override failed: got %q, want :7777", c2.Listen)
	}
}

func TestServerNoTokenRequired(t *testing.T) {
	// Server no longer requires an auth token — auth is DB-backed (Phase 4).
	c, err := LoadServer(nil)
	if err != nil {
		t.Fatalf("expected no error when BURROW_AUTH_TOKEN unset, got: %v", err)
	}
	if c.Listen != ":7000" {
		t.Fatalf("default listen = %q", c.Listen)
	}
	if c.DatabasePath != "./burrow.db" {
		t.Fatalf("default database_path = %q", c.DatabasePath)
	}
	if c.HTTPListen != ":8080" {
		t.Fatalf("default http_listen = %q", c.HTTPListen)
	}
}

func TestClientValidation(t *testing.T) {
	_, err := LoadClient(map[string]any{"server": "", "token": "x"})
	if err == nil {
		t.Fatal("expected error for empty server")
	}
	c, err := LoadClient(map[string]any{"server": "localhost:7000", "token": "x"})
	if err != nil || c.Server != "localhost:7000" {
		t.Fatalf("LoadClient: %+v err=%v", c, err)
	}
}

func TestServerPublicDefaults(t *testing.T) {
	c, err := LoadServer(nil)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if c.PublicBind != "0.0.0.0" || c.PortMin != 9000 || c.PortMax != 9100 {
		t.Fatalf("got PublicBind=%q PortMin=%d PortMax=%d", c.PublicBind, c.PortMin, c.PortMax)
	}
}

func TestServerConfigPhase4(t *testing.T) {
	t.Setenv("BURROW_ADMIN_EMAIL", "a@x")
	t.Setenv("BURROW_ADMIN_PASSWORD", "pw")
	c, err := LoadServer(nil)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if c.DatabasePath != "./burrow.db" || c.AdminEmail != "a@x" || c.AdminPassword != "pw" || c.HTTPListen != ":8080" {
		t.Fatalf("phase4 fields: %+v", c)
	}
}

func TestServerTLSRequired(t *testing.T) {
	if _, err := LoadServer(map[string]any{"tls_cert": ""}); err == nil {
		t.Fatal("empty tls_cert must fail validation")
	}
	if _, err := LoadServer(map[string]any{"tls_key": ""}); err == nil {
		t.Fatal("empty tls_key must fail validation")
	}
}

func TestServerHTTPSecureCookiesDefaultAndOverride(t *testing.T) {
	c, err := LoadServer(nil)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if c.HTTPSecureCookies {
		t.Fatalf("default http_secure_cookies must be false, got true")
	}
	t.Setenv("BURROW_HTTP_SECURE_COOKIES", "true")
	c2, err := LoadServer(nil)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if !c2.HTTPSecureCookies {
		t.Fatalf("env override BURROW_HTTP_SECURE_COOKIES=true not applied")
	}
}

// TestTrustedProxiesDefaultEmpty asserts that the default TrustedProxies is an
// empty slice (safe: no forwarded headers trusted).
func TestTrustedProxiesDefaultEmpty(t *testing.T) {
	c, err := LoadServer(nil)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if len(c.TrustedProxies) != 0 {
		t.Fatalf("default trusted_proxies must be empty, got %v", c.TrustedProxies)
	}
}

// TestTrustedProxiesEnvParseCIDRList asserts that BURROW_TRUSTED_PROXIES
// (comma-separated) is correctly split and stored as a []string.
func TestTrustedProxiesEnvParseCIDRList(t *testing.T) {
	t.Setenv("BURROW_TRUSTED_PROXIES", "10.0.0.0/8, 192.168.1.1")
	c, err := LoadServer(nil)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if len(c.TrustedProxies) != 2 {
		t.Fatalf("expected 2 entries, got %v", c.TrustedProxies)
	}
	if c.TrustedProxies[0] != "10.0.0.0/8" {
		t.Fatalf("first entry = %q, want 10.0.0.0/8", c.TrustedProxies[0])
	}
	if c.TrustedProxies[1] != "192.168.1.1" {
		t.Fatalf("second entry = %q, want 192.168.1.1", c.TrustedProxies[1])
	}
}

// TestTrustedProxiesOverrideParseCIDR asserts that an override map entry works.
func TestTrustedProxiesOverrideParseCIDR(t *testing.T) {
	c, err := LoadServer(map[string]any{"trusted_proxies": []string{"172.16.0.0/12"}})
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if len(c.TrustedProxies) != 1 || c.TrustedProxies[0] != "172.16.0.0/12" {
		t.Fatalf("expected [172.16.0.0/12], got %v", c.TrustedProxies)
	}
}

// TestTrustedProxiesInvalidCIDRError asserts that an unparseable entry causes
// LoadServer to return an error (fail-fast validation).
func TestTrustedProxiesInvalidCIDRError(t *testing.T) {
	_, err := LoadServer(map[string]any{"trusted_proxies": []string{"not-a-cidr"}})
	if err == nil {
		t.Fatal("expected error for invalid CIDR in trusted_proxies, got nil")
	}
	if !strings.Contains(err.Error(), "trusted_proxies") {
		t.Fatalf("error should mention trusted_proxies, got: %v", err)
	}
}

// writeSecret writes content to a temp file and returns its path. The file is
// cleaned up automatically when the test ends.
func writeSecret(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "secret-*")
	if err != nil {
		t.Fatalf("create temp secret: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp secret: %v", err)
	}
	f.Close()
	return f.Name()
}

// TestServerAdminPasswordFile asserts that BURROW_ADMIN_PASSWORD_FILE is read
// and its trailing newline is stripped, leaving internal content intact.
func TestServerAdminPasswordFile(t *testing.T) {
	path := writeSecret(t, "s3cret\n")
	t.Setenv("BURROW_ADMIN_PASSWORD_FILE", path)

	c, err := LoadServer(nil)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if c.AdminPassword != "s3cret" {
		t.Fatalf("admin_password = %q, want %q", c.AdminPassword, "s3cret")
	}
}

// TestServerAdminPasswordFileInternalSpacesPreserved proves that only the
// trailing newline is stripped; internal content (including spaces) is kept.
func TestServerAdminPasswordFileInternalSpacesPreserved(t *testing.T) {
	path := writeSecret(t, "pass word with spaces\n")
	t.Setenv("BURROW_ADMIN_PASSWORD_FILE", path)

	c, err := LoadServer(nil)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if c.AdminPassword != "pass word with spaces" {
		t.Fatalf("admin_password = %q, want %q", c.AdminPassword, "pass word with spaces")
	}
}

// TestServerFilePrecedenceOverLiteralEnv asserts that when both BURROW_<KEY>
// and BURROW_<KEY>_FILE are set, the _FILE value wins (Docker convention).
func TestServerFilePrecedenceOverLiteralEnv(t *testing.T) {
	path := writeSecret(t, "from-file\n")
	t.Setenv("BURROW_ADMIN_PASSWORD", "from-literal")
	t.Setenv("BURROW_ADMIN_PASSWORD_FILE", path)

	c, err := LoadServer(nil)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if c.AdminPassword != "from-file" {
		t.Fatalf("_FILE must win over literal env: got %q, want %q", c.AdminPassword, "from-file")
	}
}

// TestServerMissingFileReturnsError asserts that a BURROW_*_FILE pointing to a
// non-existent path causes LoadServer to return a hard error (fail-fast).
func TestServerMissingFileReturnsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.txt")
	t.Setenv("BURROW_ADMIN_PASSWORD_FILE", missing)

	_, err := LoadServer(nil)
	if err == nil {
		t.Fatal("expected error for missing _FILE path, got nil")
	}
	if !strings.Contains(err.Error(), "cannot read secret file") {
		t.Fatalf("error should mention 'cannot read secret file', got: %v", err)
	}
}

// TestServerFileGenericSecondKey proves genericity: a second BURROW_*_FILE
// variable (BURROW_ADMIN_EMAIL_FILE) is also resolved correctly.
func TestServerFileGenericSecondKey(t *testing.T) {
	emailPath := writeSecret(t, "file@example.com\n")
	pwPath := writeSecret(t, "filepw\n")
	t.Setenv("BURROW_ADMIN_EMAIL_FILE", emailPath)
	t.Setenv("BURROW_ADMIN_PASSWORD_FILE", pwPath)

	c, err := LoadServer(nil)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if c.AdminEmail != "file@example.com" {
		t.Fatalf("admin_email = %q, want %q", c.AdminEmail, "file@example.com")
	}
	if c.AdminPassword != "filepw" {
		t.Fatalf("admin_password = %q, want %q", c.AdminPassword, "filepw")
	}
}

// TestServerOverridesStillBeatFile asserts that explicit programmatic overrides
// win over _FILE values (final precedence: defaults<litenv<_FILE<overrides).
func TestServerOverridesStillBeatFile(t *testing.T) {
	path := writeSecret(t, "from-file\n")
	t.Setenv("BURROW_ADMIN_PASSWORD_FILE", path)

	c, err := LoadServer(map[string]any{"admin_password": "from-override"})
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if c.AdminPassword != "from-override" {
		t.Fatalf("override must beat _FILE: got %q, want %q", c.AdminPassword, "from-override")
	}
}

// TestClientTokenFile asserts that BURROW_TOKEN_FILE is resolved for the
// client config path (proving _FILE works in LoadClient too).
func TestClientTokenFile(t *testing.T) {
	path := writeSecret(t, "bur_tok123\n")
	t.Setenv("BURROW_TOKEN_FILE", path)

	c, err := LoadClient(map[string]any{"server": "localhost:7000"})
	if err != nil {
		t.Fatalf("LoadClient: %v", err)
	}
	if c.Token != "bur_tok123" {
		t.Fatalf("token = %q, want %q", c.Token, "bur_tok123")
	}
}

// TestClientMissingFileReturnsError asserts fail-fast behavior in LoadClient.
func TestClientMissingFileReturnsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-secret.txt")
	t.Setenv("BURROW_TOKEN_FILE", missing)

	_, err := LoadClient(map[string]any{"server": "localhost:7000"})
	if err == nil {
		t.Fatal("expected error for missing _FILE in LoadClient, got nil")
	}
	if !strings.Contains(err.Error(), "cannot read secret file") {
		t.Fatalf("error should mention 'cannot read secret file', got: %v", err)
	}
}
