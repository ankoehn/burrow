// Package redact is Burrow's deterministic PII / secret redaction engine.
//
// Layout:
//
//   - rules.go    — Rule struct + the bundled BuiltIn rule pack.
//   - engine.go   — Engine: precompiled regexes + Apply.
//   - presidio.go — optional Tier-2 hook (analyze + anonymize HTTP calls).
//
// API:
//
//	eng, err := NewEngine(custom)
//	out, dropped, hits, err := eng.Apply(body, "request_body")
//
// Semantics:
//
//   - Apply returns the rewritten body. Drop rules short-circuit and the
//     caller is responsible for emitting 400 redaction.drop. The "logs only"
//     mode is a caller concern: the engine always returns the redacted body,
//     and the caller decides whether to forward the original body to upstream
//     or the rewritten body (see the locked invariant in the v0.4.0 plan).
//   - Rules iterate in name order so Apply is deterministic across runs.
//   - For each rule, all non-overlapping matches are rewritten left-to-right.
//     Once one rule has rewritten a span, subsequent rules see the rewritten
//     output (so chaining can never re-redact the literal "[redacted: …]"
//     marker — there are no PII patterns inside the marker text).
//
// Performance notes:
//
//   - All regexes are precompiled once at NewEngine time. Apply runs
//     allocation-light: one []byte builder per rule that fires.
//   - Luhn validation runs only after a credit_card_luhn regex match (so the
//     hot path doesn't pay for it on rule-cold bodies).
package redact

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
)

// RuleHit records one rule firing against a body, with the number of
// matches it rewrote. The proxy/audit pipeline (Task 10) emits these as
// the `redactions` field on the per-request usage record (spec wire shape
// in web/src/lib/contract.ts: { rule, count }).
type RuleHit struct {
	Rule  Rule `json:"rule"`
	Count int  `json:"count"`
}

// compiledRule is a Rule + its precompiled regex. Internal to the engine.
type compiledRule struct {
	Rule
	re *regexp.Regexp
}

// Engine is the redaction engine: a slice of compiled rules ordered by name,
// so Apply is deterministic. Construct with NewEngine; do not mutate after
// construction.
type Engine struct {
	rules []compiledRule
}

// NewEngine validates and compiles the union of BuiltIn ++ custom rules
// into an Engine. Any invalid regex, unknown action, or unknown scope in
// a custom rule causes the constructor to fail with an explanatory error.
// Built-in rules are trusted (we author them); they are validated for
// belt-and-suspenders only.
//
// Custom rules with the same ID as a built-in are rejected (the wire ID
// space must stay disjoint; the JSON-API handler enforces this on
// POST/PUT, but the constructor refuses it too so the engine cannot be
// constructed in a contradictory state).
func NewEngine(custom []Rule) (*Engine, error) {
	all := make([]Rule, 0, len(BuiltIn)+len(custom))
	all = append(all, BuiltIn...)
	for _, r := range custom {
		if IsBuiltIn(r.ID) {
			return nil, fmt.Errorf("redact: custom rule id %q collides with built-in", r.ID)
		}
		all = append(all, r)
	}
	// Sort by name for deterministic iteration. Stable so two rules with the
	// same name (shouldn't happen, but be defensive) keep their insertion order.
	sort.SliceStable(all, func(i, j int) bool { return all[i].Name < all[j].Name })

	out := make([]compiledRule, 0, len(all))
	for _, r := range all {
		if r.Pattern == "" {
			return nil, fmt.Errorf("redact: rule %q: empty pattern", r.Name)
		}
		if !validActions[r.Action] {
			return nil, fmt.Errorf("redact: rule %q: unknown action %q", r.Name, r.Action)
		}
		if !validScopes[r.Scope] {
			return nil, fmt.Errorf("redact: rule %q: unknown scope %q", r.Name, r.Scope)
		}
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return nil, fmt.Errorf("redact: rule %q: invalid regex: %w", r.Name, err)
		}
		out = append(out, compiledRule{Rule: r, re: re})
	}
	return &Engine{rules: out}, nil
}

// Apply runs every rule whose scope matches the caller's scope ("both" always
// matches) against body, in deterministic (name) order. Returns:
//
//   - out:     the rewritten body. When no rules fire, out == body (same
//     underlying slice — caller must not mutate). When at least one rule
//     fires, out is a fresh allocation.
//   - dropped: non-nil pointer to the first drop-action rule that matched.
//     Apply short-circuits as soon as a drop rule fires; out is whatever
//     was rewritten so far (the caller's contract is to discard it and emit
//     400 redaction.drop, but we keep it valid for debugging).
//   - hits:    one RuleHit per rule that rewrote at least one match. Order
//     matches rule iteration order (name).
//   - err:     non-nil only on internal failure (none today; reserved for
//     future hash-impl changes).
//
// Apply is safe for concurrent use after NewEngine returns — the compiled
// rules are read-only and *regexp.Regexp is concurrency-safe.
func (e *Engine) Apply(body []byte, scope string) (out []byte, dropped *Rule, hits []RuleHit, err error) {
	if e == nil || len(e.rules) == 0 {
		return body, nil, nil, nil
	}
	out = body
	for i := range e.rules {
		r := e.rules[i]
		if !ruleScopeMatches(r.Scope, scope) {
			continue
		}
		// Find all non-overlapping matches.
		idxs := r.re.FindAllIndex(out, -1)
		if len(idxs) == 0 {
			continue
		}

		// For credit_card_luhn, post-filter matches through the Luhn algorithm.
		if r.Name == "credit_card_luhn" {
			idxs = filterLuhn(out, idxs)
			if len(idxs) == 0 {
				continue
			}
		}

		if r.Action == ActionDrop {
			ruleCopy := r.Rule
			return out, &ruleCopy, append(hits, RuleHit{Rule: r.Rule, Count: len(idxs)}), nil
		}

		// mask | hash: rewrite each match span in-place into a fresh buffer.
		rewritten := rewriteSpans(out, idxs, func(match []byte) []byte {
			switch r.Action {
			case ActionMask:
				return maskReplacement(r.Name)
			case ActionHash:
				return hashReplacement(match)
			default:
				return match // unreachable: validated at NewEngine
			}
		})
		out = rewritten
		hits = append(hits, RuleHit{Rule: r.Rule, Count: len(idxs)})
	}
	return out, nil, hits, nil
}

// ruleScopeMatches reports whether a rule's declared scope fires under the
// caller's scope. "both" matches anything; otherwise the scopes must be equal.
// An unknown caller scope (defensive) never matches anything other than
// "both" rules.
func ruleScopeMatches(ruleScope, callerScope string) bool {
	if ruleScope == ScopeBoth {
		return true
	}
	return ruleScope == callerScope
}

// maskReplacement is the literal bullet-prefixed marker the spec mandates
// for mask-action rules: "••• [redacted: <rule_name>]". The bullet is the
// UTF-8 byte sequence for U+2022; included as a literal here so the source
// matches the spec without depending on \u escapes at write time.
func maskReplacement(name string) []byte {
	return []byte("••• [redacted: " + name + "]")
}

// hashReplacement is sha256(value) truncated to the first 10 hex chars.
// Stable across runs (same input → same output), so audit logs and
// inspector traces produce reproducible fingerprints for the same value.
func hashReplacement(value []byte) []byte {
	sum := sha256.Sum256(value)
	enc := hex.EncodeToString(sum[:])
	if len(enc) < 10 {
		return []byte(enc) // unreachable
	}
	return []byte(enc[:10])
}

// rewriteSpans constructs a fresh byte slice with each [start,end) span in
// idxs replaced by replace(orig[start:end]). idxs must come from
// re.FindAllIndex on src, which guarantees non-overlapping ascending pairs.
func rewriteSpans(src []byte, idxs [][]int, replace func([]byte) []byte) []byte {
	// Compute exact final length to allocate the buffer once.
	finalLen := len(src)
	replacements := make([][]byte, len(idxs))
	for i, span := range idxs {
		start, end := span[0], span[1]
		repl := replace(src[start:end])
		replacements[i] = repl
		finalLen += len(repl) - (end - start)
	}
	out := make([]byte, 0, finalLen)
	cursor := 0
	for i, span := range idxs {
		start, end := span[0], span[1]
		out = append(out, src[cursor:start]...)
		out = append(out, replacements[i]...)
		cursor = end
	}
	out = append(out, src[cursor:]...)
	return out
}

// filterLuhn returns the subset of idxs whose matched substring passes the
// Luhn check. The Luhn algorithm is documented at
// https://en.wikipedia.org/wiki/Luhn_algorithm — we use the standard
// "double-every-second-from-rightmost" form.
func filterLuhn(src []byte, idxs [][]int) [][]int {
	out := idxs[:0]
	for _, span := range idxs {
		if luhnValid(src[span[0]:span[1]]) {
			out = append(out, span)
		}
	}
	// Defensive copy: shrinking the slice in-place is fine for the caller,
	// but if no matches survive we still want a zero-len slice (out == idxs[:0]
	// already has that shape).
	return out
}

// luhnValid strips spaces and dashes from value, then applies the Luhn
// checksum. Returns true on a valid 13-16 digit card-shaped number.
//
// We deliberately reject anything shorter than 13 or longer than 16 digits:
// the bundled credit_card_luhn regex allows 13-16 digit runs, but separator
// characters can change the match length, so we re-check the digit count.
func luhnValid(value []byte) bool {
	// Strip non-digits.
	digits := make([]byte, 0, len(value))
	for _, b := range value {
		switch {
		case b >= '0' && b <= '9':
			digits = append(digits, b-'0')
		case b == ' ' || b == '-':
			// permitted separators
		default:
			return false // anything else is not a credit card
		}
	}
	n := len(digits)
	if n < 13 || n > 16 {
		return false
	}
	sum := 0
	// "Double every second digit from the right (starting one place LEFT
	// of the rightmost)." Equivalent: digits at 0-indexed position i are
	// doubled when (n - 1 - i) is odd. Algebra: that condition simplifies
	// to i%2 == n%2 (since (n-1-i) odd ⇔ (n-i) even ⇔ i%2 == n%2).
	parity := n % 2
	for i, d := range digits {
		dd := int(d)
		if i%2 == parity {
			dd *= 2
			if dd > 9 {
				dd -= 9
			}
		}
		sum += dd
	}
	return sum%10 == 0
}
