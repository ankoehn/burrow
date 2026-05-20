// Package config loads server/client configuration: defaults < env < _FILE env < overrides.
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/v2"
)

var validate = validator.New()

// TunnelSpec is one tunnel the client asks the server to register.
type TunnelSpec struct {
	Name       string `koanf:"name"`
	LocalAddr  string `koanf:"local_addr" validate:"required,hostname_port"`
	RemotePort int    `koanf:"remote_port" validate:"gte=0,lte=65535"`
}

// ServerConfig configures burrowd.
type ServerConfig struct {
	Listen            string `koanf:"listen" validate:"required"`
	TLSCert           string `koanf:"tls_cert" validate:"required"`
	TLSKey            string `koanf:"tls_key" validate:"required"`
	LogLevel          string `koanf:"log_level"`
	LogFormat         string `koanf:"log_format"`
	PublicBind        string `koanf:"public_bind"`
	PortMin           int    `koanf:"port_min" validate:"gte=1,lte=65535"`
	PortMax           int    `koanf:"port_max" validate:"gte=1,lte=65535,gtefield=PortMin"`
	DatabasePath      string `koanf:"database_path"`
	AdminEmail        string `koanf:"admin_email"`
	AdminPassword     string `koanf:"admin_password"`
	HTTPListen        string `koanf:"http_listen"`
	HTTPSecureCookies bool   `koanf:"http_secure_cookies"`
	// HTTPTLSCert and HTTPTLSKey control native TLS for the HTTP API/dashboard.
	// Both must be set together (both-or-neither); if exactly one is set,
	// LoadServer returns an error. When both are set the HTTP server is served
	// over HTTPS using these certificates (distinct from the control-plane
	// TLSCert/TLSKey); when both are empty (the default) the server serves
	// plain HTTP and the operator is expected to terminate TLS at a proxy.
	// Env: BURROW_HTTP_TLS_CERT / BURROW_HTTP_TLS_KEY (also _FILE variants).
	HTTPTLSCert string `koanf:"http_tls_cert"`
	HTTPTLSKey  string `koanf:"http_tls_key"`
	// TrustedProxies is the list of CIDRs or IP addresses of reverse proxies
	// whose X-Forwarded-For / X-Real-IP headers the server will honor.
	//
	// Empty (default) means NO forwarded headers are trusted: the raw TCP peer
	// address is always used as the client IP. This is the safe default for a
	// direct-internet deployment and prevents XFF spoofing from bypassing the
	// per-IP login rate-limiter or poisoning session IP records.
	//
	// Non-empty: the server honors forwarded headers ONLY when the immediate
	// TCP peer IP is within one of the listed CIDRs. Set to your load-balancer
	// or proxy IP/CIDR (e.g. "10.0.0.0/8") when deploying behind a proxy.
	// Env: BURROW_TRUSTED_PROXIES (comma-separated CIDRs/IPs).
	TrustedProxies []string `koanf:"trusted_proxies"`
	// SMTPPassword is the SMTP auth password for the dashboard mailer. It is a
	// SECRET: sourced from BURROW_SMTP_PASSWORD or BURROW_SMTP_PASSWORD_FILE
	// (the _FILE form wins, handled by applyFileSecrets) and is NEVER written
	// to the settings table. The non-secret SMTP fields (host/port/username/
	// from/tls) live in the settings table instead. Empty = SMTP unconfigured.
	// Env: BURROW_SMTP_PASSWORD (also _FILE variant).
	SMTPPassword string `koanf:"smtp_password"`
	// HTTPProxyListen is the TCP address the HTTP reverse-proxy listener binds
	// to. Defaults to ":8443". An empty string disables the proxy listener.
	// Env: BURROW_HTTP_PROXY_LISTEN.
	HTTPProxyListen string `koanf:"http_proxy_listen"`
	// HTTPProxyTLSCert and HTTPProxyTLSKey are the TLS certificate and key files
	// for the HTTP reverse-proxy listener. Both must be set together
	// (both-or-neither); if exactly one is set, LoadServer returns an error.
	// When both are empty (the default) the proxy listener runs without TLS and
	// the operator is expected to terminate TLS upstream.
	// Env: BURROW_HTTP_PROXY_TLS_CERT / BURROW_HTTP_PROXY_TLS_KEY (also _FILE variants).
	HTTPProxyTLSCert string `koanf:"http_proxy_tls_cert"`
	HTTPProxyTLSKey  string `koanf:"http_proxy_tls_key"`
	// AuthDomain is the domain used as the authentication boundary for the HTTP
	// reverse proxy (e.g. "tunnels.example.com"). Empty by default; Task 12 will
	// use this to construct per-tunnel subdomains.
	// Env: BURROW_AUTH_DOMAIN.
	AuthDomain string `koanf:"auth_domain"`

	// ----- v0.4.0 additions (Task 24) ---------------------------------------

	// MCPListen is the TCP address the optional `burrowd mcp` JSON-RPC listener
	// binds to (spec Part P). Defaults to "" — listener disabled. ":7800" is
	// the conventional bind-all form. Env: BURROW_MCP_LISTEN.
	MCPListen string `koanf:"mcp_listen"`
	// BurrowMCPToken is the plaintext bua_-prefixed bearer token the MCP
	// listener authenticates with. Empty (default) means the listener falls
	// back to bearer-token lookup against the automation_tokens table. SECRET
	// — sourced from BURROW_MCP_TOKEN or BURROW_MCP_TOKEN_FILE (the _FILE form
	// wins, handled by applyFileSecrets) and never written to the settings
	// table. Env: BURROW_MCP_TOKEN (also _FILE variant).
	//
	// koanf key is "mcp_token" — derived from BURROW_MCP_TOKEN via the
	// prefix-strip + lowercase rule. The Go field name keeps the "Burrow"
	// stem to match the plan's Type contract.
	BurrowMCPToken string `koanf:"mcp_token"`
	// GeoDBPath is the on-disk path to a MaxMind GeoLite2 database file used
	// by the geo-restriction access mode (spec Q11). Empty (default) selects
	// the NoopGeoLookup — geo features are inert. Env: BURROW_GEO_DB_PATH.
	GeoDBPath string `koanf:"geo_db_path"`
	// PricingPath, when non-empty, overrides the embedded pricing.yaml the
	// cost engine ships with (spec Part F). Empty (default) uses the embedded
	// file. Env: BURROW_PRICING_PATH (also _FILE variant — the _FILE form
	// reads a file CONTAINING the path, kept for consistency with the
	// generic _FILE pattern).
	PricingPath string `koanf:"pricing_path"`
	// WebAuthnRPID is the Relying Party ID — a BARE hostname (no scheme, no
	// port, no path) per the WebAuthn spec. When unset it is derived at
	// LoadServer time:
	//   - if AuthDomain set → AuthDomain;
	//   - else            → host portion of HTTPListen (port stripped); if
	//                       the host is empty (e.g. ":8080") it falls back
	//                       to "localhost".
	// Validation rejects values containing "://" (operator passed a URL by
	// mistake). Env: BURROW_WEBAUTHN_RP_ID.
	WebAuthnRPID string `koanf:"webauthn_rp_id"`
	// WebAuthnRPName is the human-readable Relying Party display name shown
	// in the browser's WebAuthn prompt. Defaults to "Burrow". Env:
	// BURROW_WEBAUTHN_RP_NAME.
	WebAuthnRPName string `koanf:"webauthn_rp_name"`
	// WebAuthnOrigin is the FULL scheme://host[:port] URL the browser will
	// send in clientDataJSON. The WebAuthn library verifies an EXACT match,
	// so no trailing slash and no path component are permitted. When unset
	// it is derived at LoadServer time:
	//   - if AuthDomain set → "https://" + AuthDomain (production assumes TLS);
	//   - else            → "http://" + derived RPID + ":" + HTTPListen port;
	//                       when HTTPListen has no port it falls back to host.
	// Validation requires http:// or https:// prefix, no trailing slash, and
	// no path. Env: BURROW_WEBAUTHN_ORIGIN.
	WebAuthnOrigin string `koanf:"webauthn_origin"`
	// BackupDir is the on-disk directory the backup API and `burrowd backup`
	// CLI write/read archives in (Task 20). Defaults to "<DatabasePath>.backups"
	// so a stock deployment gets a working JSON API out of the box. When set
	// to a relative path it is resolved against the process working directory
	// at use time. Env: BURROW_BACKUP_DIR.
	BackupDir string `koanf:"backup_dir"`
}

// ClientConfig configures burrow.
type ClientConfig struct {
	Server     string       `koanf:"server" validate:"required,hostname_port"`
	Token      string       `koanf:"token" validate:"required"`
	Insecure   bool         `koanf:"insecure"`
	CACert     string       `koanf:"cacert"`
	ServerName string       `koanf:"server_name"`
	Tunnels    []TunnelSpec `koanf:"tunnels"`
	LogLevel   string       `koanf:"log_level"`
	LogFormat  string       `koanf:"log_format"`
}

func base() *koanf.Koanf { return koanf.New(".") }

func envProvider(prefix string) *env.Env {
	return env.Provider(prefix, ".", func(s string) string {
		return strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(s, prefix)), "__", ".")
	})
}

// burrowEnvProvider is like envProvider but additionally splits
// BURROW_TRUSTED_PROXIES (a comma-separated list) into a []string so that
// koanf can unmarshal it into ServerConfig.TrustedProxies correctly.
func burrowEnvProvider() *env.Env {
	return env.ProviderWithValue("BURROW_", ".", func(rawKey, rawVal string) (string, interface{}) {
		key := strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(rawKey, "BURROW_")), "__", ".")
		if key == "trusted_proxies" {
			if rawVal == "" {
				return key, []string{}
			}
			parts := strings.Split(rawVal, ",")
			out := make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					out = append(out, p)
				}
			}
			return key, out
		}
		return key, rawVal
	})
}

// normalizeEnvKey maps a raw env-var name (without the BURROW_ prefix) to the
// koanf key the env providers use: lowercase + double-underscore to dot.
// This MUST match the transform in both envProvider and burrowEnvProvider so
// that _FILE keys resolve to exactly the same koanf keys as the literal vars.
func normalizeEnvKey(rawSuffix string) string {
	return strings.ReplaceAll(strings.ToLower(rawSuffix), "__", ".")
}

// applyFileSecrets scans the process environment for variables of the form
// BURROW_<KEY>_FILE. For each one it reads the file at the referenced path,
// trims a single trailing newline (Docker/Swarm secrets conventionally append
// one), and sets the corresponding koanf key in k.
//
// Precedence: this layer sits AFTER the literal env provider, so a _FILE value
// WINS over a literal BURROW_<KEY> set in the environment. A missing or
// unreadable file returns a hard error (fail-fast; silent fallback would leave
// the server unseeded).
func applyFileSecrets(k *koanf.Koanf) error {
	const prefix = "BURROW_"
	const fileSuffix = "_FILE"

	overrides := map[string]any{}
	for _, kv := range os.Environ() {
		eqIdx := strings.IndexByte(kv, '=')
		if eqIdx < 0 {
			continue
		}
		name, path := kv[:eqIdx], kv[eqIdx+1:]

		// Must start with BURROW_ and end with _FILE.
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, fileSuffix) {
			continue
		}

		// Strip the BURROW_ prefix and the _FILE suffix to get the raw key suffix.
		rawSuffix := strings.TrimSuffix(strings.TrimPrefix(name, prefix), fileSuffix)
		if rawSuffix == "" {
			// Edge-case: "BURROW__FILE" — no meaningful key; skip.
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("config: %s=%q: cannot read secret file: %w", name, path, err)
		}

		// Trim only a single trailing \r\n or \n (Docker file-secrets convention).
		// Internal content (spaces, special chars) is intentionally left intact.
		value := strings.TrimRight(string(data), "\r\n")

		koanfKey := normalizeEnvKey(rawSuffix)
		overrides[koanfKey] = value
	}
	if len(overrides) == 0 {
		return nil
	}
	return k.Load(confmap.Provider(overrides, "."), nil)
}

// deriveV04Fields fills in the four post-merge derived defaults for v0.4.0:
// WebAuthnRPID, WebAuthnOrigin, and BackupDir. Called only after koanf has
// merged defaults < env < _FILE < overrides so the derivations see the
// final values for HTTPListen / AuthDomain / DatabasePath.
//
// Precedence: an explicit value (env or override) always wins; this only
// fills the blanks. Empty fields after derivation indicate a misconfigured
// HTTPListen (no host/port at all) — the next validation pass will catch
// that case.
func deriveV04Fields(c *ServerConfig) {
	// --- WebAuthn RPID ------------------------------------------------------
	if c.WebAuthnRPID == "" {
		if c.AuthDomain != "" {
			c.WebAuthnRPID = c.AuthDomain
		} else {
			host, _, err := net.SplitHostPort(c.HTTPListen)
			if err != nil || host == "" {
				host = "localhost"
			}
			c.WebAuthnRPID = host
		}
	}
	// --- WebAuthn Origin ----------------------------------------------------
	if c.WebAuthnOrigin == "" {
		if c.AuthDomain != "" {
			// Production: HTTPS at the auth boundary is the safe assumption.
			c.WebAuthnOrigin = "https://" + c.AuthDomain
		} else {
			// Local/dev: derive from HTTPListen. The host portion of the
			// origin must match the RPID exactly (or be a subdomain of it),
			// so we reuse the already-derived RPID for the host. Append the
			// port from HTTPListen when present.
			_, port, err := net.SplitHostPort(c.HTTPListen)
			if err != nil || port == "" {
				c.WebAuthnOrigin = "http://" + c.WebAuthnRPID
			} else {
				c.WebAuthnOrigin = "http://" + c.WebAuthnRPID + ":" + port
			}
		}
	}
	// --- BackupDir ----------------------------------------------------------
	if c.BackupDir == "" {
		c.BackupDir = c.DatabasePath + ".backups"
	}
}

// validateV04Fields checks the spec-mandated shape of the v0.4.0 fields:
//
//   - WebAuthnRPID:   bare hostname (no scheme, no "://").
//   - WebAuthnOrigin: http(s):// scheme, no trailing slash, no path.
//   - MCPListen:      empty (disabled) or parseable as [host]:port.
//
// Returns the first violation as an error.
func validateV04Fields(c *ServerConfig) error {
	// RP ID must NOT contain a URL scheme. The library will reject it later
	// too, but fail fast at config-load time so misconfiguration is loud.
	if strings.Contains(c.WebAuthnRPID, "://") {
		return fmt.Errorf("webauthn_rp_id must be a bare hostname, not a URL (no scheme/\"://\"): %q", c.WebAuthnRPID)
	}
	// Origin must be a clean http(s):// URL with no trailing slash and no path.
	switch {
	case strings.HasPrefix(c.WebAuthnOrigin, "http://"), strings.HasPrefix(c.WebAuthnOrigin, "https://"):
		// fall through to the structural checks below.
	default:
		return fmt.Errorf("webauthn_origin must start with http:// or https://, got %q", c.WebAuthnOrigin)
	}
	if strings.HasSuffix(c.WebAuthnOrigin, "/") {
		return fmt.Errorf("webauthn_origin must not have a trailing slash: %q", c.WebAuthnOrigin)
	}
	// Strip the scheme and look for a remaining "/" — that would be a path.
	rest := strings.TrimPrefix(strings.TrimPrefix(c.WebAuthnOrigin, "https://"), "http://")
	if strings.ContainsRune(rest, '/') {
		return fmt.Errorf("webauthn_origin must not contain a path component: %q", c.WebAuthnOrigin)
	}
	// MCP listener: empty means disabled. Non-empty must parse as [host]:port
	// AND the port must be numeric in the 1-65535 range — net.SplitHostPort by
	// itself accepts symbolic service names like ":http", which we want to
	// reject at config-load time so operator typos fail loudly.
	if c.MCPListen != "" {
		_, port, err := net.SplitHostPort(c.MCPListen)
		if err != nil {
			return fmt.Errorf("mcp_listen %q is not a valid [host]:port: %w", c.MCPListen, err)
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("mcp_listen %q has invalid numeric port", c.MCPListen)
		}
	}
	return nil
}

// parseTrustedProxies validates that each entry in the list is a valid CIDR or
// IP address. Returns an error naming the first invalid entry.
func parseTrustedProxies(entries []string) error {
	for _, e := range entries {
		if e == "" {
			continue
		}
		// Accept both bare IPs and CIDR notation.
		if _, _, err := net.ParseCIDR(e); err != nil {
			if net.ParseIP(e) == nil {
				return fmt.Errorf("trusted_proxies: %q is not a valid CIDR or IP address", e)
			}
		}
	}
	return nil
}

// LoadServer loads the server config, merging defaults < BURROW_ env < _FILE env < overrides.
//
// If both BURROW_<KEY> and BURROW_<KEY>_FILE are set the _FILE value wins
// (Docker convention); the literal env is loaded first, then _FILE secrets
// overwrite it. Explicit programmatic overrides still win over everything.
// A non-existent or unreadable _FILE path is a hard error.
func LoadServer(overrides map[string]any) (*ServerConfig, error) {
	k := base()
	_ = k.Load(confmap.Provider(map[string]any{
		"listen": ":7000", "tls_cert": "certs/dev-server.pem",
		"tls_key": "certs/dev-server-key.pem", "log_level": "info", "log_format": "text",
		"public_bind": "0.0.0.0", "port_min": 9000, "port_max": 9100,
		// database_path is resolved relative to the process working directory;
		// supply an absolute path via BURROW_DATABASE_PATH in production.
		"database_path": "./burrow.db", "http_listen": ":8080", "http_secure_cookies": false,
		// trusted_proxies defaults to empty: no forwarded headers trusted.
		"trusted_proxies": []string{},
		// http_proxy_listen defaults to :8443; empty string disables the listener.
		// http_proxy_tls_cert/key default to empty (no TLS; operator terminates upstream).
		// auth_domain defaults to empty (no subdomain-routing configured).
		"http_proxy_listen": ":8443", "http_proxy_tls_cert": "", "http_proxy_tls_key": "", "auth_domain": "",
		// v0.4.0 (Task 24) defaults. The empty-string defaults disable the
		// associated feature (MCP listener, geo lookup, pricing override).
		// WebAuthn RP id/origin and BackupDir default to empty here and are
		// derived after unmarshalling — once HTTPListen/AuthDomain/DatabasePath
		// have been resolved through env+_FILE+overrides — so all derivation
		// rules see the final post-merge values.
		"mcp_listen": "", "burrow_mcp_token": "", "geo_db_path": "", "pricing_path": "",
		"webauthn_rp_id": "", "webauthn_rp_name": "Burrow", "webauthn_origin": "",
		"backup_dir": "",
	}, "."), nil)
	_ = k.Load(burrowEnvProvider(), nil)
	if err := applyFileSecrets(k); err != nil {
		return nil, err
	}
	if overrides != nil {
		_ = k.Load(confmap.Provider(overrides, "."), nil)
	}
	var c ServerConfig
	if err := k.Unmarshal("", &c); err != nil {
		return nil, fmt.Errorf("unmarshal server config: %w", err)
	}
	if err := validate.Struct(&c); err != nil {
		return nil, fmt.Errorf("invalid server config: %w", err)
	}
	if err := parseTrustedProxies(c.TrustedProxies); err != nil {
		return nil, fmt.Errorf("invalid server config: %w", err)
	}
	// Both-or-neither validation for HTTP TLS cert/key pair (xor is invalid).
	if (c.HTTPTLSCert == "") != (c.HTTPTLSKey == "") {
		return nil, fmt.Errorf("invalid server config: http_tls_cert and http_tls_key must both be set or both be empty")
	}
	// Both-or-neither validation for HTTP proxy TLS cert/key pair (xor is invalid).
	if (c.HTTPProxyTLSCert == "") != (c.HTTPProxyTLSKey == "") {
		return nil, fmt.Errorf("invalid server config: http_proxy_tls_cert and http_proxy_tls_key must both be set or both be empty")
	}
	// v0.4.0 (Task 24): derive WebAuthn RP id/origin and BackupDir from the
	// already-merged HTTPListen/AuthDomain/DatabasePath, then validate the
	// new field shapes. Derivation must come BEFORE validation so the
	// derived origin is the one we sanity-check.
	deriveV04Fields(&c)
	if err := validateV04Fields(&c); err != nil {
		return nil, fmt.Errorf("invalid server config: %w", err)
	}
	return &c, nil
}

// LoadClient loads the client config, merging defaults < BURROW_ env < _FILE env < overrides.
//
// If both BURROW_<KEY> and BURROW_<KEY>_FILE are set the _FILE value wins.
// A non-existent or unreadable _FILE path is a hard error.
func LoadClient(overrides map[string]any) (*ClientConfig, error) {
	k := base()
	_ = k.Load(confmap.Provider(map[string]any{
		"log_level": "info", "log_format": "text",
	}, "."), nil)
	_ = k.Load(envProvider("BURROW_"), nil)
	if err := applyFileSecrets(k); err != nil {
		return nil, err
	}
	if overrides != nil {
		_ = k.Load(confmap.Provider(overrides, "."), nil)
	}
	var c ClientConfig
	if err := k.Unmarshal("", &c); err != nil {
		return nil, fmt.Errorf("unmarshal client config: %w", err)
	}
	if err := validate.Struct(&c); err != nil {
		return nil, fmt.Errorf("invalid client config: %w", err)
	}
	return &c, nil
}
