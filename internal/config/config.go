// Package config loads server/client configuration: defaults < env < overrides.
package config

import (
	"fmt"
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
	Listen     string `koanf:"listen" validate:"required"`
	TLSCert    string `koanf:"tls_cert" validate:"required"`
	TLSKey     string `koanf:"tls_key" validate:"required"`
	AuthToken  string `koanf:"auth_token" validate:"required"`
	LogLevel   string `koanf:"log_level"`
	LogFormat  string `koanf:"log_format"`
	PublicBind string `koanf:"public_bind"`
	PortMin    int    `koanf:"port_min" validate:"gte=1,lte=65535"`
	PortMax    int    `koanf:"port_max" validate:"gte=1,lte=65535,gtefield=PortMin"`
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

// LoadServer loads the server config, merging defaults < BURROW_ env < overrides.
func LoadServer(overrides map[string]any) (*ServerConfig, error) {
	k := base()
	_ = k.Load(confmap.Provider(map[string]any{
		"listen": ":7000", "tls_cert": "certs/dev-server.pem",
		"tls_key": "certs/dev-server-key.pem", "log_level": "info", "log_format": "text",
		"public_bind": "0.0.0.0", "port_min": 9000, "port_max": 9100,
	}, "."), nil)
	_ = k.Load(envProvider("BURROW_"), nil)
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
	return &c, nil
}

// LoadClient loads the client config, merging defaults < BURROW_ env < overrides.
func LoadClient(overrides map[string]any) (*ClientConfig, error) {
	k := base()
	_ = k.Load(confmap.Provider(map[string]any{
		"log_level": "info", "log_format": "text",
	}, "."), nil)
	_ = k.Load(envProvider("BURROW_"), nil)
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
