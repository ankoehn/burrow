package config

import "testing"

func TestServerDefaultsAndEnvOverride(t *testing.T) {
	t.Setenv("BURROW_AUTH_TOKEN", "envtok")
	c, err := LoadServer(nil)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if c.Listen != ":7000" {
		t.Fatalf("default listen = %q", c.Listen)
	}
	if c.AuthToken != "envtok" {
		t.Fatalf("env override failed: %q", c.AuthToken)
	}
}

func TestServerValidationRequiresToken(t *testing.T) {
	if _, err := LoadServer(nil); err == nil {
		t.Fatal("expected validation error when BURROW_AUTH_TOKEN unset")
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
	t.Setenv("BURROW_AUTH_TOKEN", "x")
	c, err := LoadServer(nil)
	if err != nil {
		t.Fatalf("LoadServer: %v", err)
	}
	if c.PublicBind != "0.0.0.0" || c.PortMin != 9000 || c.PortMax != 9100 {
		t.Fatalf("got PublicBind=%q PortMin=%d PortMax=%d", c.PublicBind, c.PortMin, c.PortMax)
	}
}
