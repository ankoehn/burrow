// Package exact implements Burrow's v0.4.0 exact-match prompt cache: a
// deterministic canonicalisation function that turns the request
// (method/scheme/host/path/headers/body) into a stable byte string, hashed
// to a key the caller scopes with an applies_per prefix (global /
// endpoint:<svc>:<path> / apikey:<id>), plus a SQLite-backed Lookup/Store
// engine that honours TTL + LRU eviction.
package exact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// HeaderAllowlist is the closed set of request headers that participate in
// the canonical cache key. Everything else (including the configured API-key
// header — that one is explicitly excluded so cache can be shared across keys
// when applies_per: global) is dropped before hashing.
//
// Names are lowercase (HTTP header names are case-insensitive — RFC 9110
// §5.1). Callers MUST pass already-lowercased names to lookup the allowlist.
var HeaderAllowlist = map[string]bool{
	"accept":            true,
	"content-type":      true,
	"anthropic-version": true,
}

// excludedBodyKeys is the closed set of JSON body keys dropped before
// canonicalising. `stream` flips the request to SSE which is never cached;
// `n` requests multiple completions and so two requests with different `n`
// are not semantically equal even though everything else matches.
var excludedBodyKeys = map[string]bool{
	"stream": true,
	"n":      true,
}

// CanonicaliseInput is the request shape Canonicalise hashes. The caller
// (proxy hot path, Task 10) constructs this from the in-flight *http.Request
// before issuing the upstream call, so the exact bytes are stable across the
// lookup-then-store round-trip.
type CanonicaliseInput struct {
	// Method is the HTTP method (e.g. "POST"). ASCII-lowercased before use.
	Method string
	// Scheme is the URL scheme (e.g. "https"). ASCII-lowercased before use.
	Scheme string
	// Host is the request Host (URL or Host header). ASCII-lowercased before use.
	Host string
	// Path is the request URL path (after subdomain stripping, before query).
	// Not lowercased (paths are case-sensitive per RFC 3986).
	Path string
	// Headers is the raw request header map (canonical-case keys allowed —
	// Canonicalise lowercases internally). Only HeaderAllowlist names survive.
	Headers map[string][]string
	// Body is the raw request body bytes. JSON bodies are parsed +
	// re-marshalled (Go's encoding/json sorts map keys deterministically since
	// 1.12); non-JSON bodies pass through unchanged.
	Body []byte
	// APIKeyHeader is the configured per-service api_key header name (default
	// "Authorization"). Excluded from Headers + dropped from JSON body keys.
	APIKeyHeader string
}

// Canonicalise returns the deterministic byte string that becomes the cache
// key after sha256. Two semantically-equal requests (whitespace differences,
// JSON key reordering) yield identical bytes; ergo identical hashes.
//
// The format is documented inline so a reviewer can audit the algorithm
// without leaving the file:
//
//	<method_lower>\n<scheme_lower>\n<host_lower>\n<path>\n<canonical_headers>\n<canonical_body>
//
// canonical_headers is the allowlisted headers joined as "name=value\n",
// sorted by name (one line per name; multi-value headers are joined on \x00
// to keep ordering significant).
//
// canonical_body is the JSON-canonicalised body when Content-Type starts with
// application/json (excludedBodyKeys + the API-key header name dropped),
// otherwise the body bytes unchanged.
func Canonicalise(in CanonicaliseInput) []byte {
	method := strings.ToLower(in.Method)
	scheme := strings.ToLower(in.Scheme)
	host := strings.ToLower(in.Host)
	path := in.Path

	// Headers: lowercase name, keep only allowlisted, sort by name, join values
	// on \x00 (a byte that cannot appear in an HTTP header value per RFC 9110
	// §5.5 — so ordering is preserved without ambiguity).
	apiKeyHeaderLower := strings.ToLower(in.APIKeyHeader)
	type kv struct{ k, v string }
	var hdrs []kv
	for name, vals := range in.Headers {
		lower := strings.ToLower(name)
		if !HeaderAllowlist[lower] {
			continue
		}
		if lower == apiKeyHeaderLower {
			// Belt-and-braces: the API-key header is excluded even when its
			// configured name collides with an allowlisted name (e.g. someone
			// re-uses Authorization). Cache MUST be sharable across keys at
			// applies_per: global, per spec §B.3.
			continue
		}
		hdrs = append(hdrs, kv{k: lower, v: strings.Join(vals, "\x00")})
	}
	sort.Slice(hdrs, func(i, j int) bool { return hdrs[i].k < hdrs[j].k })
	var headerBuf strings.Builder
	for _, h := range hdrs {
		headerBuf.WriteString(h.k)
		headerBuf.WriteByte('=')
		headerBuf.WriteString(h.v)
		headerBuf.WriteByte('\n')
	}

	// Body: JSON re-marshal when Content-Type is application/json so key order
	// and whitespace differences collapse. Other content types pass through.
	body := canonicaliseBody(in)

	var out strings.Builder
	out.WriteString(method)
	out.WriteByte('\n')
	out.WriteString(scheme)
	out.WriteByte('\n')
	out.WriteString(host)
	out.WriteByte('\n')
	out.WriteString(path)
	out.WriteByte('\n')
	out.WriteString(headerBuf.String())
	out.WriteByte('\n')
	out.Write(body)
	return []byte(out.String())
}

// canonicaliseBody returns the body bytes used in the canonical key. For
// JSON content-types, the body is unmarshalled into a map[string]any (so
// Go's json.Marshal sorts keys deterministically since 1.12), with
// excludedBodyKeys + the API-key header name removed. Malformed JSON falls
// back to the raw bytes — losing the cross-request determinism for that
// payload, but never failing the proxy.
func canonicaliseBody(in CanonicaliseInput) []byte {
	if len(in.Body) == 0 {
		return nil
	}
	ct := ""
	for name, vals := range in.Headers {
		if strings.ToLower(name) == "content-type" && len(vals) > 0 {
			ct = vals[0]
			break
		}
	}
	if !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		return in.Body
	}
	var obj map[string]any
	if err := json.Unmarshal(in.Body, &obj); err != nil {
		// Non-object JSON or malformed: cannot key-sort, pass through.
		return in.Body
	}
	for k := range excludedBodyKeys {
		delete(obj, k)
	}
	if in.APIKeyHeader != "" {
		delete(obj, in.APIKeyHeader)
		delete(obj, strings.ToLower(in.APIKeyHeader))
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return in.Body
	}
	return b
}

// HashKey returns the hex-encoded sha256 of the canonical bytes. The full
// cache key the caller stores/looks up is "<scope_prefix>:<hex>" where
// scope_prefix is one of:
//
//	global
//	endpoint:<service_id>:<path>
//	apikey:<api_key_id>
//
// Constructing that prefix is the caller's responsibility (Task 10 wiring)
// because the cache engine never resolves an api_key.id by itself.
func HashKey(canonical []byte) string {
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}
