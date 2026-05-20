package guardrails

import (
	"fmt"
	"regexp"
)

// compiled is one Pattern paired with its precompiled *regexp.Regexp.
// Engine holds these in iteration order so Inspect can short-circuit on
// the first match without re-sorting.
type compiled struct {
	Pattern
	re *regexp.Regexp
}

// Engine is the prompt-injection regex tier. Construct with NewEngine
// (compiles every bundled pattern at startup); call Inspect on every
// inbound request body the caller wants to screen.
//
// Engine is concurrency-safe after NewEngine returns: the compiled rules
// are read-only and *regexp.Regexp is itself goroutine-safe.
type Engine struct {
	rules []compiled
}

// NewEngine compiles the bundled Patterns list and returns a ready-to-use
// Engine. Every bundled regex is author-vetted; a compile failure is a
// programming error (and would already break tests at CI time), so
// NewEngine panics on compile failure rather than returning err — the
// caller (cmd/server) does not need an err return path for a closed
// build-time pattern set.
//
// Pattern iteration order matches Patterns declaration order: Inspect
// returns the first match it finds, so high-priority rules are listed
// first in patterns.go.
func NewEngine() *Engine {
	out := make([]compiled, 0, len(Patterns))
	for _, p := range Patterns {
		re, err := regexp.Compile(p.Regex)
		if err != nil {
			// Closed pattern set, compiled at startup — a failure here is
			// a code defect (caught by TestPatterns_AllCompile). Panic
			// makes the process refuse to start rather than silently miss
			// a rule.
			panic(fmt.Sprintf("guardrails: invalid bundled regex for %q: %v", p.ID, err))
		}
		out = append(out, compiled{Pattern: p, re: re})
	}
	return &Engine{rules: out}
}

// Inspect screens body against every bundled pattern in declaration
// order, returning (true, pattern.ID) on the first match. body may be
// nil or empty — both yield (false, "").
//
// Inspect does NOT allocate a copy of body; it passes the input directly
// to *regexp.Regexp.Match, which is safe to call on a shared slice (the
// caller still owns the byte buffer).
//
// The returned ID is the *stable* identifier (Pattern.ID), NOT the regex
// source. Audit and inspector consumers store this ID verbatim so the
// regex source can evolve without breaking historical logs.
func (e *Engine) Inspect(body []byte) (hit bool, pattern string) {
	if e == nil || len(body) == 0 {
		return false, ""
	}
	for i := range e.rules {
		if e.rules[i].re.Match(body) {
			return true, e.rules[i].ID
		}
	}
	return false, ""
}
