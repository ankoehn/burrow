package mcpserv

import (
	"crypto/sha256"
	"encoding/hex"
)

// sha256Hex returns the lowercase hex sha256 of the supplied plaintext.
// Matches api.sha256Hex and store.AutomationTokenView.Hash construction so
// the BearerStore lookup keys agree across the two surfaces.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
