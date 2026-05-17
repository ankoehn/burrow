package auth

import (
	"crypto/rand"
	"encoding/hex"
)

// generateRandomToken returns hex of n cryptographically-random bytes.
func generateRandomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
