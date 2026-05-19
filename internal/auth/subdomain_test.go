package auth

import (
	"strings"
	"testing"
)

func TestGenerateSubdomain(t *testing.T) {
	s, err := GenerateSubdomain()
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 6 {
		t.Fatalf("want 6 chars, got %q", s)
	}
	for _, r := range s {
		if !strings.ContainsRune("abcdefghijkmnpqrstuvwxyz23456789", r) {
			t.Fatalf("char %q outside the safe alphabet", r)
		}
	}
	a, _ := GenerateSubdomain()
	b, _ := GenerateSubdomain()
	if a == b {
		t.Fatalf("not random: %q == %q", a, b)
	}
}
