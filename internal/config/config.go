// Package config loads server/client configuration: defaults < env < _FILE env < overrides.
package config

import (
	"fmt"
	"net"
	"os"
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
