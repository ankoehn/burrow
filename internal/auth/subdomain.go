package auth

import (
	"crypto/rand"
	"math/big"
)

// subdomainAlphabet is the 31-character safe alphabet for subdomains.
// Ambiguous characters l, o, 0, and 1 are excluded to prevent confusion.
const subdomainAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

// GenerateSubdomain returns a random 6-character subdomain string drawn
// uniformly from a 31-character alphabet (a–z without l/o, 2–9 without 0/1).
//
// This function is collision-unaware: the caller (service store) must retry
// on a UNIQUE constraint failure when persisting the subdomain.
// Excluded ambiguous characters: l, o, 0, 1.
func GenerateSubdomain() (string, error) {
	n := big.NewInt(int64(len(subdomainAlphabet)))
	buf := make([]byte, 6)
	for i := range buf {
		idx, err := rand.Int(rand.Reader, n)
		if err != nil {
			return "", err
		}
		buf[i] = subdomainAlphabet[idx.Int64()]
	}
	return string(buf), nil
}
