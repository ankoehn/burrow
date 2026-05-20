package inspector

import (
	"fmt"
	"strings"
)

// IsTextualContentType reports whether the given Content-Type header value
// looks like text the unified-diff path can render. Empty / unknown values
// default to non-text so binary fall-through wins on uncertainty.
func IsTextualContentType(ct string) bool {
	if ct == "" {
		return false
	}
	// Strip parameters: "text/html; charset=utf-8" → "text/html".
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	ct = strings.TrimSpace(strings.ToLower(ct))
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	switch ct {
	case "application/json",
		"application/xml",
		"application/javascript",
		"application/yaml",
		"application/x-yaml",
		"application/x-www-form-urlencoded",
		"application/sql":
		return true
	}
	// Many vendor JSON types end in "+json" or "+xml".
	if strings.HasSuffix(ct, "+json") || strings.HasSuffix(ct, "+xml") {
		return true
	}
	return false
}

// UnifiedBody renders a small line-by-line unified diff of two textual
// bodies. The result is single-block (no hunk headers), prefixed with
// "--- original\n+++ replayed\n" so callers can render it directly. Lines
// unchanged on both sides are prefixed with a single space; removed with
// "-", added with "+".
//
// This is intentionally tiny (200 lines max per the plan): the result is
// not meant to be diff(1)-byte-compatible — it's a human-readable summary
// of the two responses. For full-fidelity diffs the UI can re-render on
// the client.
func UnifiedBody(original, replayed []byte) string {
	a := splitLines(string(original))
	b := splitLines(string(replayed))
	var sb strings.Builder
	sb.WriteString("--- original\n")
	sb.WriteString("+++ replayed\n")
	// Longest-Common-Subsequence-based unified diff. We keep it O(n*m) which
	// is fine for the 256KB body cap (≈64K lines worst case).
	ops := lcsDiff(a, b)
	for _, op := range ops {
		switch op.kind {
		case opEqual:
			sb.WriteByte(' ')
			sb.WriteString(op.line)
			sb.WriteByte('\n')
		case opDel:
			sb.WriteByte('-')
			sb.WriteString(op.line)
			sb.WriteByte('\n')
		case opAdd:
			sb.WriteByte('+')
			sb.WriteString(op.line)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// MetadataOnlyBody returns the binary-fallback diff text described in the
// plan: a single line summarising the two byte lengths.
func MetadataOnlyBody(originalLen, replayedLen int) string {
	return fmt.Sprintf("<binary content; original %d bytes, replayed %d bytes>",
		originalLen, replayedLen)
}

// HeadersDiff returns a sorted list of "key: old → new" entries listing
// every header that differs between original and replayed (added, removed,
// or changed). Each entry is a single line, ready to be joined by the
// caller into the JSON array. Header names are compared case-insensitively
// (matching Go's net/http canonicalization).
func HeadersDiff(original, replayed map[string]string) []string {
	keys := map[string]struct{}{}
	for k := range original {
		keys[strings.ToLower(k)] = struct{}{}
	}
	for k := range replayed {
		keys[strings.ToLower(k)] = struct{}{}
	}
	// Build a stable order.
	ks := make([]string, 0, len(keys))
	for k := range keys {
		ks = append(ks, k)
	}
	// Insertion sort is fine; the typical case has <40 headers.
	for i := 1; i < len(ks); i++ {
		for j := i; j > 0 && ks[j] < ks[j-1]; j-- {
			ks[j], ks[j-1] = ks[j-1], ks[j]
		}
	}
	out := make([]string, 0, len(ks))
	for _, k := range ks {
		o, oOK := lookupCI(original, k)
		n, nOK := lookupCI(replayed, k)
		if oOK && nOK && o == n {
			continue
		}
		switch {
		case oOK && nOK:
			out = append(out, k+": "+o+" → "+n)
		case oOK:
			out = append(out, "-"+k+": "+o)
		default:
			out = append(out, "+"+k+": "+n)
		}
	}
	return out
}

// lookupCI does a case-insensitive lookup against a map keyed by arbitrary
// header casing.
func lookupCI(m map[string]string, lowerKey string) (string, bool) {
	if v, ok := m[lowerKey]; ok {
		return v, true
	}
	for k, v := range m {
		if strings.EqualFold(k, lowerKey) {
			return v, true
		}
	}
	return "", false
}

// splitLines is strings.Split(s, "\n") but trims a trailing empty line that
// shows up when s ends with "\n" — diff(1) treats that as no extra line.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

const (
	opEqual = iota
	opDel
	opAdd
)

type diffOp struct {
	kind int
	line string
}

// lcsDiff computes a minimal LCS-based diff between a and b. The result is
// a sequence of opEqual/opDel/opAdd ops describing how to transform a → b.
func lcsDiff(a, b []string) []diffOp {
	n, m := len(a), len(b)
	// Build LCS DP table.
	// Rows: 0..n ; Cols: 0..m.
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	// Walk back to recover ops in reverse.
	var rev []diffOp
	i, j := n, m
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			rev = append(rev, diffOp{kind: opEqual, line: a[i-1]})
			i--
			j--
		} else if dp[i-1][j] >= dp[i][j-1] {
			rev = append(rev, diffOp{kind: opDel, line: a[i-1]})
			i--
		} else {
			rev = append(rev, diffOp{kind: opAdd, line: b[j-1]})
			j--
		}
	}
	for i > 0 {
		rev = append(rev, diffOp{kind: opDel, line: a[i-1]})
		i--
	}
	for j > 0 {
		rev = append(rev, diffOp{kind: opAdd, line: b[j-1]})
		j--
	}
	// Reverse in place.
	out := make([]diffOp, len(rev))
	for k, op := range rev {
		out[len(rev)-1-k] = op
	}
	return out
}
