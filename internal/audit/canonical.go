// Package audit implements Burrow's append-only, hash-chained audit log
// (spec Part G).
//
// Every mutation that the spec marks as auditable is appended through
// Logger.Append, which computes hash = sha256(prev_hash_bytes ||
// canonical(this-without-hash)). The previous hash is the hash of the most
// recent row (genesis = 64 zero hex chars), so any single-row tamper
// invalidates the chain from that point forward.
//
// Canonical encoding is a json.Marshal over a map[string]any whose keys are
// every audit_events column EXCEPT hash and prev_hash. Go's json.Marshal
// sorts map keys alphabetically, giving a stable deterministic byte
// representation. The payload column is decoded then re-marshalled as
// map[string]any so its nested keys are also sorted (the chain hash is
// robust against client JSON key-order differences).
//
// Exports are NDJSON: one JSON object per audit row, in id order, followed
// by a single trailer {"_signature": "<base64 ed25519 sig over all preceding
// bytes>", "fingerprint": "<sha256 hex of the public key>"}. The signing
// key is generated once at first boot and stored in
// settings.audit.signing_key — Verify and ExportNDJSON load it from there.
package audit

import (
	"encoding/json"
	"fmt"
	"time"
)

// Canonical returns the canonical JSON encoding of e for hash-chain input.
//
// Per spec Part G.1, hash = sha256(prev_hash_bytes || canonical(this)).
// Canonical contains every audit_events column EXCEPT hash and prev_hash
// (prev_hash is concatenated separately so it is not in the map).
//
// json.Marshal sorts map keys alphabetically since Go 1.12, so the byte
// output is deterministic. Payload is decoded into map[string]any and
// re-marshalled with this map so its nested keys are also alphabetised —
// without this the chain would diverge for two semantically identical
// payloads that arrived with different key orders.
func Canonical(e Event) ([]byte, error) {
	var payload any
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return nil, fmt.Errorf("canonical: decode payload: %w", err)
		}
	}
	// Build the map. Every value is json-encodable; json.Marshal sorts the
	// map's top-level keys alphabetically and recursively walks maps the
	// same way, so the payload's nested keys are sorted too.
	m := map[string]any{
		"action":        e.Action,
		"actor_email":   e.ActorEmail,
		"actor_id":      e.ActorID,
		"id":            e.ID,
		"payload":       payload,
		"request_id":    e.RequestID,
		"result":        e.Result,
		"source_ip":     e.SourceIP,
		"subject_id":    e.SubjectID,
		"subject_label": e.SubjectLabel,
		"ts":            e.TS.UTC().Format(time.RFC3339Nano),
		"user_agent":    e.UserAgent,
	}
	return json.Marshal(m)
}
