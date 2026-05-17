package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewTextAndLevel(t *testing.T) {
	var buf bytes.Buffer
	lg := NewTo(&buf, "info", "text")
	lg.Debug("nope")
	lg.Info("yep", "k", "v")
	out := buf.String()
	if strings.Contains(out, "nope") {
		t.Fatal("debug should be filtered at info level")
	}
	if !strings.Contains(out, "yep") || !strings.Contains(out, "k=v") {
		t.Fatalf("expected text line with attrs, got %q", out)
	}
}

func TestNewJSON(t *testing.T) {
	var buf bytes.Buffer
	NewTo(&buf, "debug", "json").Info("hello")
	if !strings.HasPrefix(strings.TrimSpace(buf.String()), "{") {
		t.Fatalf("expected JSON, got %q", buf.String())
	}
}
