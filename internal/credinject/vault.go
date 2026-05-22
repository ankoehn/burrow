// Package credinject implements upstream-credential injection for Burrow's
// AI reverse-proxy: an env-only secret vault (spec B.1) and a per-service
// request injector (spec B.3).
package credinject

import (
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strings"
)

// slotRegexp is the allowed shape for a slot handle: uppercase letters,
// digits, and underscores; 1-32 characters (spec B.1).
var slotRegexp = regexp.MustCompile(`^[A-Z0-9_]{1,32}$`)

// envPrefix is the env-var prefix that identifies upstream key vars.
const envPrefix = "BURROW_UPSTREAM_KEY_"

// Vault is the read surface for upstream credential slots.
// The only production implementation is *EnvVault, constructed once at
// startup; tests provide inline stubs.
type Vault interface {
	// Get returns the plaintext key for the named slot. Never logs the value.
	// Returns ("", false) when the slot is not present.
	Get(slot string) (string, bool)
	// Slots returns the sorted list of registered slot handles.
	Slots() []string
}

// EnvVault is the env-only implementation of Vault (spec B.1). It scans
// os.Environ() once at construction time and holds the result in a read-only
// map — no mutex needed after construction.
//
// Env-var shapes recognised:
//
//	BURROW_UPSTREAM_KEY_<SLOT>=<plaintext>      — literal value
//	BURROW_UPSTREAM_KEY_<SLOT>_FILE=<filepath>  — read file; trim trailing whitespace
//
// If both are present the _FILE value wins (matches the applyFileSecrets rule
// in internal/config: _FILE overrides the literal env). Slot handles that do
// not match [A-Z0-9_]{1,32} are silently ignored.
type EnvVault struct {
	m     map[string]string // slot → plaintext; read-only after New()
	slots []string          // sorted slot names
}

// NewEnvVault scans os.Environ() and returns a populated vault.
func NewEnvVault() *EnvVault {
	return newEnvVaultFrom(os.Environ())
}

// newEnvVaultFrom is the testable inner constructor (accepts an explicit env slice).
func newEnvVaultFrom(environ []string) *EnvVault {
	// Two-pass: first collect literal values, then overwrite with _FILE values.
	m := make(map[string]string)

	for _, kv := range environ {
		eqIdx := strings.IndexByte(kv, '=')
		if eqIdx < 0 {
			continue
		}
		name, val := kv[:eqIdx], kv[eqIdx+1:]
		if !strings.HasPrefix(name, envPrefix) {
			continue
		}
		suffix := strings.TrimPrefix(name, envPrefix)
		// _FILE variant?
		if strings.HasSuffix(suffix, "_FILE") {
			slot := strings.TrimSuffix(suffix, "_FILE")
			if !slotRegexp.MatchString(slot) {
				continue
			}
			data, err := os.ReadFile(val)
			if err != nil {
				// Missing/unreadable _FILE: skip the slot.
				continue
			}
			// Trim trailing whitespace (Docker file-secret convention).
			m[slot] = strings.TrimRight(string(data), "\r\n \t")
		} else {
			slot := suffix
			if !slotRegexp.MatchString(slot) {
				continue
			}
			// Only store if not already overridden by a _FILE value, but
			// since we do a second pass for _FILE keys below — we simply
			// record it now; a later _FILE entry overwrites it.
			if _, alreadyFile := m[slot]; !alreadyFile {
				m[slot] = val
			}
		}
	}

	// Second pass: _FILE overrides — re-scan to set them unconditionally.
	for _, kv := range environ {
		eqIdx := strings.IndexByte(kv, '=')
		if eqIdx < 0 {
			continue
		}
		name, val := kv[:eqIdx], kv[eqIdx+1:]
		if !strings.HasPrefix(name, envPrefix) {
			continue
		}
		suffix := strings.TrimPrefix(name, envPrefix)
		if !strings.HasSuffix(suffix, "_FILE") {
			continue
		}
		slot := strings.TrimSuffix(suffix, "_FILE")
		if !slotRegexp.MatchString(slot) {
			continue
		}
		data, err := os.ReadFile(val)
		if err != nil {
			continue
		}
		m[slot] = strings.TrimRight(string(data), "\r\n \t")
	}

	slots := make([]string, 0, len(m))
	for k := range m {
		slots = append(slots, k)
	}
	sort.Strings(slots)
	return &EnvVault{m: m, slots: slots}
}

// Get returns the plaintext key for the named slot.
func (v *EnvVault) Get(slot string) (string, bool) {
	val, ok := v.m[slot]
	return val, ok
}

// Slots returns the sorted list of slot handles.
func (v *EnvVault) Slots() []string {
	out := make([]string, len(v.slots))
	copy(out, v.slots)
	return out
}

// LogValue implements slog.LogValuer so accidental slog.Info("v", "v", vault)
// calls never leak secrets into logs.
func (v *EnvVault) LogValue() slog.Value { return slog.StringValue("<vault redacted>") }
