package audit

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

// SettingsKey is the row key in the settings table where the Ed25519
// private key for audit exports is persisted (base64-encoded).
const SettingsKey = "audit.signing_key"

// SettingsStore is the narrow read/write surface Logger needs to load (and
// on first boot, persist) the Ed25519 signing key. *store.Store satisfies
// it via Get/SaveSettings.
type SettingsStore interface {
	GetSettings(ctx context.Context) (map[string]string, error)
	SaveSettings(ctx context.Context, kv map[string]string) error
}

// LoadOrGenerateSigningKey reads the audit signing key from settings; if
// no row exists it generates a fresh ed25519 keypair, persists the private
// key (base64-encoded) and returns it. Returns the private key; callers
// derive the public key via PrivateKey.Public().
//
// The key MUST NOT appear in any GET /api/v1/settings response — the API
// layer's settings handler filters audit.* keys out.
func LoadOrGenerateSigningKey(ctx context.Context, ss SettingsStore) (ed25519.PrivateKey, error) {
	m, err := ss.GetSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("audit: load settings: %w", err)
	}
	if enc := m[SettingsKey]; enc != "" {
		raw, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			return nil, fmt.Errorf("audit: decode signing key: %w", err)
		}
		if l := len(raw); l != ed25519.PrivateKeySize {
			return nil, fmt.Errorf("audit: signing key wrong size: got %d want %d", l, ed25519.PrivateKeySize)
		}
		return ed25519.PrivateKey(raw), nil
	}
	// First boot: generate + persist.
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("audit: generate signing key: %w", err)
	}
	enc := base64.StdEncoding.EncodeToString(priv)
	if err := ss.SaveSettings(ctx, map[string]string{SettingsKey: enc}); err != nil {
		return nil, fmt.Errorf("audit: persist signing key: %w", err)
	}
	return priv, nil
}

// Fingerprint returns the lowercase hex SHA-256 of pub. NDJSON export
// trailers include this so a consumer can pin the key without trusting
// the server out-of-band.
func Fingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])
}

// trailer is the JSON shape of the final NDJSON line emitted by
// ExportNDJSON. Fields are deliberately underscore-prefixed (_signature) so
// any consumer that maps NDJSON lines into a typed event struct can
// distinguish trailer from event without a separate marker.
type trailer struct {
	Signature   string `json:"_signature"`
	Fingerprint string `json:"fingerprint"`
}

// ErrEmptySigningKey is returned by helpers that would otherwise sign with
// an uninitialised key. Tests construct Loggers via NewLogger which always
// loads/generates a key, so this is a defensive guard only.
var ErrEmptySigningKey = errors.New("audit: signing key not initialised")
