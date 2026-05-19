package main

import (
	"strings"
	"testing"
)

// TestConnectCmdHasTypeFlag verifies the --type flag is present with default "tcp".
func TestConnectCmdHasTypeFlag(t *testing.T) {
	cmd := newConnectCmd()
	f := cmd.Flags().Lookup("type")
	if f == nil {
		t.Fatal("--type flag missing")
	}
	if f.DefValue != "tcp" {
		t.Fatalf("default = %q, want tcp", f.DefValue)
	}
}

// TestConnectCmdRejectsUnknownType verifies that --type=xyz returns an error.
func TestConnectCmdRejectsUnknownType(t *testing.T) {
	_, err := buildTunnelSpec("", 0, "", "xyz")
	if err == nil {
		t.Fatal("expected error for unknown type, got nil")
	}
	if !strings.Contains(err.Error(), "unknown") && !strings.Contains(err.Error(), "type") {
		t.Fatalf("error message %q should mention type or unknown", err.Error())
	}
}

// TestConnectCmdHTTPTypePassesThrough verifies that --type=http is plumbed into TunnelSpec.Type.
func TestConnectCmdHTTPTypePassesThrough(t *testing.T) {
	spec, err := buildTunnelSpec("myapp", 0, "127.0.0.1:3000", "http")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Type != "http" {
		t.Fatalf("TunnelSpec.Type = %q, want http", spec.Type)
	}
}

// TestConnectCmdTCPDefaultPassesThrough verifies the default "tcp" type is plumbed correctly.
func TestConnectCmdTCPDefaultPassesThrough(t *testing.T) {
	spec, err := buildTunnelSpec("web", 9000, "127.0.0.1:3000", "tcp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Type != "tcp" {
		t.Fatalf("TunnelSpec.Type = %q, want tcp", spec.Type)
	}
	if spec.Name != "web" {
		t.Fatalf("TunnelSpec.Name = %q, want web", spec.Name)
	}
	if spec.RemotePort != 9000 {
		t.Fatalf("TunnelSpec.RemotePort = %d, want 9000", spec.RemotePort)
	}
	if spec.LocalAddr != "127.0.0.1:3000" {
		t.Fatalf("TunnelSpec.LocalAddr = %q, want 127.0.0.1:3000", spec.LocalAddr)
	}
}
