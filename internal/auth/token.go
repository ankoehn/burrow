package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// GenerateClientToken returns a new opaque client token (shown once) and its
// sha256-hex hash (the only thing persisted). The bur_ prefix enables future
// secret scanning.
func GenerateClientToken() (plaintext, hashed string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	plaintext = "bur_" + base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(plaintext))
	hashed = hex.EncodeToString(sum[:])
	return plaintext, hashed, nil
}

// HashToken returns the sha256-hex of a plaintext client token (for lookup).
func HashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// GenerateAPIKey returns a new opaque service API key (shown once) and its
// sha256-hex hash (the only thing persisted). The buk_ prefix enables future
// secret scanning and distinguishes service API keys from client tokens (bur_).
func GenerateAPIKey() (plaintext, hashed string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	plaintext = "buk_" + base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(plaintext))
	hashed = hex.EncodeToString(sum[:])
	return plaintext, hashed, nil
}
