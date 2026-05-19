package main

import (
	"os"
	"path/filepath"
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

// TestConnectCmdConfigFlagExists verifies the --config flag is present on the connect command.
func TestConnectCmdConfigFlagExists(t *testing.T) {
	cmd := newConnectCmd()
	f := cmd.Flags().Lookup("config")
	if f == nil {
		t.Fatal("--config flag missing from connect command")
	}
}

// TestConnectCmdConfigConflictsWithSingleFlags verifies that --config combined with
// any single-tunnel flag (--server, --token, --local, --remote, --name, --type) is rejected.
func TestConnectCmdConfigConflictsWithSingleFlags(t *testing.T) {
	// Write a minimal valid burrow.yaml for this test.
	dir := t.TempDir()
	yml := filepath.Join(dir, "burrow.yaml")
	if err := os.WriteFile(yml, []byte("server: relay.example.com:7000\ntoken: tok\nservices:\n  - { name: app, local: 127.0.0.1:3000 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	conflictFlags := []string{"server", "token", "local", "name", "type"}
	for _, flag := range conflictFlags {
		t.Run("conflict_with_"+flag, func(t *testing.T) {
			cmd := newConnectCmd()
			// Set --config and the conflicting flag.
			args := []string{"--config=" + yml, "--" + flag + "=somevalue"}
			cmd.SetArgs(args)
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error when --config combined with --%s, got nil", flag)
			}
			if !strings.Contains(err.Error(), flag) && !strings.Contains(err.Error(), "config") {
				t.Fatalf("error %q should mention %q or config", err.Error(), flag)
			}
		})
	}
	// --remote is an int flag, test separately
	t.Run("conflict_with_remote", func(t *testing.T) {
		cmd := newConnectCmd()
		cmd.SetArgs([]string{"--config=" + yml, "--remote=9000"})
		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error when --config combined with --remote, got nil")
		}
		if !strings.Contains(err.Error(), "remote") && !strings.Contains(err.Error(), "config") {
			t.Fatalf("error %q should mention remote or config", err.Error())
		}
	})
}
