package auth

import (
	"strings"
	"testing"
)

func TestHashVerifyPassword(t *testing.T) {
	h, err := HashPassword("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$") {
		t.Fatalf("bad encoding: %s", h)
	}
	ok, err := VerifyPassword("s3cret", h)
	if err != nil || !ok {
		t.Fatalf("correct password should verify: ok=%v err=%v", ok, err)
	}
	ok, err = VerifyPassword("wrong", h)
	if err != nil || ok {
		t.Fatalf("wrong password must fail: ok=%v err=%v", ok, err)
	}
	if _, err := VerifyPassword("x", "not-a-hash"); err == nil {
		t.Fatal("malformed hash must error")
	}
	h2, _ := HashPassword("s3cret")
	if h2 == h {
		t.Fatal("salts must differ → different encodings")
	}
}

func TestClientToken(t *testing.T) {
	pt, hash, err := GenerateClientToken()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pt, "bur_") {
		t.Fatalf("token must have bur_ prefix: %s", pt)
	}
	if len(hash) != 64 {
		t.Fatalf("sha256 hex must be 64 chars, got %d", len(hash))
	}
	if HashToken(pt) != hash {
		t.Fatal("HashToken must reproduce the stored hash")
	}
	pt2, _, _ := GenerateClientToken()
	if pt2 == pt {
		t.Fatal("tokens must be unique")
	}
}

func TestRandomToken(t *testing.T) {
	a, err := generateRandomToken(32)
	if err != nil || len(a) != 64 {
		t.Fatalf("32 bytes → 64 hex chars: got %d err=%v", len(a), err)
	}
	b, _ := generateRandomToken(32)
	if a == b {
		t.Fatal("must be random")
	}
}

func TestGenerateAPIKey(t *testing.T) {
	pt, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pt, "buk_") {
		t.Fatalf("api key must have buk_ prefix: %s", pt)
	}
	if len(hash) != 64 {
		t.Fatalf("sha256 hex must be 64 chars, got %d", len(hash))
	}
	if HashToken(pt) != hash {
		t.Fatal("HashToken must reproduce the stored hash")
	}
	// Client tokens and API keys must be distinct (different prefixes).
	clientPt, _, _ := GenerateClientToken()
	if strings.HasPrefix(clientPt, "buk_") {
		t.Fatal("client token must not have buk_ prefix")
	}
	// Two API keys must be unique.
	pt2, _, _ := GenerateAPIKey()
	if pt2 == pt {
		t.Fatal("api keys must be unique")
	}
}
