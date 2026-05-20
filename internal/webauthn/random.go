package webauthn

import "crypto/rand"

// defaultReadRandom delegates to crypto/rand.Read. Wrapped behind a function
// indirection so tests can swap the source via the readRandom package var
// without importing crypto/rand themselves.
func defaultReadRandom(b []byte) (int, error) { return rand.Read(b) }
