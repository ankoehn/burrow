package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// APIKeyValidator is the narrow interface the accessChecker uses to validate
// API keys. *store.Store satisfies it implicitly (same method signature).
// Tests use a local fake instead of pulling in the full store.
type APIKeyValidator interface {
	ValidateAPIKey(ctx context.Context, serviceID, presented string) (bool, error)
}

// accessChecker implements AccessChecker for all three access modes:
// "open", "api_key", and "burrow_login".
//
// Design note — burrow_login redirect:
// The checker is constructed with authDomain so it can build the full gate
// URL (https://<authDomain>/__burrow/login?next=<escaped original URL>)
// and return status=302 + Location header. The Proxy's ServeHTTP already
// propagates hdr headers and writes the status code; no proxy.go change is
// required for redirect support.
type accessChecker struct {
	v          APIKeyValidator
	authDomain string
	log        *slog.Logger
}

// NewAccessChecker returns an AccessChecker that enforces the access policy
// described by Resolved.AccessMode on every request.
//
//   - v:          API key validator (e.g. *store.Store). Must not be nil for
//     api_key mode; ignored for open and burrow_login modes.
//   - authDomain: the Burrow auth/ingress domain (e.g. "tunnels.example.com").
//     Required for burrow_login redirects; may be empty only if no service
//     uses burrow_login mode (the store layer rejects that at write time).
func NewAccessChecker(v APIKeyValidator, authDomain string) AccessChecker {
	return &accessChecker{
		v:          v,
		authDomain: authDomain,
		log:        slog.Default(),
	}
}

// NewAccessCheckerWithLogger is like NewAccessChecker but uses a caller-supplied
// logger. Used internally when the Proxy wires itself.
func NewAccessCheckerWithLogger(v APIKeyValidator, authDomain string, log *slog.Logger) AccessChecker {
	return &accessChecker{
		v:          v,
		authDomain: authDomain,
		log:        log,
	}
}

// Allow enforces the access mode policy for a single request.
//
// Return values follow the AccessChecker contract:
//   - ok=true:  let the request proceed to the upstream.
//   - ok=false: write (status, body, hdr) and return; do not proxy.
func (ac *accessChecker) Allow(ctx context.Context, res *Resolved, r *http.Request) (ok bool, status int, body string, hdr http.Header) {
	switch res.AccessMode {
	case "open":
		return true, 0, "", nil

	case "api_key":
		return ac.checkAPIKey(ctx, res, r)

	case "burrow_login":
		return ac.checkBurrowLogin(r)

	default:
		h := make(http.Header)
		h.Set("Content-Type", "application/json")
		return false, http.StatusInternalServerError, `{"error":"unknown access mode"}`, h
	}
}

// checkAPIKey enforces api_key mode. It reads the API key from the configured
// header (defaulting to Authorization), strips a "Bearer " prefix when using
// the default header, then delegates validation to the APIKeyValidator.
func (ac *accessChecker) checkAPIKey(ctx context.Context, res *Resolved, r *http.Request) (bool, int, string, http.Header) {
	headerName := res.APIKeyHeader
	if headerName == "" {
		headerName = "Authorization"
	}

	raw := r.Header.Get(headerName)
	presented := raw
	if headerName == "Authorization" {
		presented = parseBearer(raw)
	} else {
		presented = strings.TrimSpace(raw)
	}

	if presented == "" {
		h := make(http.Header)
		h.Set("Content-Type", "application/json")
		h.Set("WWW-Authenticate", "Bearer")
		return false, http.StatusUnauthorized, `{"error":"missing api key"}`, h
	}

	valid, err := ac.v.ValidateAPIKey(ctx, res.ServiceID, presented)
	if err != nil {
		// Log at warn; do NOT expose internal error details to the visitor.
		ac.log.Warn("api key validation error", "service_id", res.ServiceID, "err", err)
		h := make(http.Header)
		h.Set("Content-Type", "application/json")
		return false, http.StatusUnauthorized, `{"error":"invalid api key"}`, h
	}
	if !valid {
		h := make(http.Header)
		h.Set("Content-Type", "application/json")
		return false, http.StatusUnauthorized, `{"error":"invalid api key"}`, h
	}
	return true, 0, "", nil
}

// checkBurrowLogin enforces burrow_login mode by 302-redirecting the visitor
// to the gate's login page with the original URL encoded in the "next" query
// parameter. The gate URL format is:
//
//	https://<authDomain>/__burrow/login?next=<url.QueryEscape(originalURL)>
//
// where originalURL = "https://" + r.Host + r.RequestURI.
//
// If authDomain is empty (should never happen — the store layer rejects
// burrow_login without a configured auth_domain at write time), we fail
// closed with 500.
func (ac *accessChecker) checkBurrowLogin(r *http.Request) (bool, int, string, http.Header) {
	if ac.authDomain == "" {
		h := make(http.Header)
		h.Set("Content-Type", "application/json")
		return false, http.StatusInternalServerError, `{"error":"burrow_login requires auth_domain"}`, h
	}

	originalURL := "https://" + r.Host + r.RequestURI
	gateURL := "https://" + ac.authDomain + "/__burrow/login?next=" + url.QueryEscape(originalURL)

	h := make(http.Header)
	h.Set("Location", gateURL)
	return false, http.StatusFound, "", h
}

// parseBearer extracts the token from an Authorization header value that
// follows the "Bearer <token>" scheme. Returns the trimmed token, or an empty
// string when the header is absent, empty, or does not carry a Bearer token.
//
// Matching is case-sensitive on the "Bearer " prefix (single space) per common
// practice and the spec's intent to be OpenAI-client-friendly.
func parseBearer(s string) string {
	const prefix = "Bearer "
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, prefix) {
		return strings.TrimSpace(s[len(prefix):])
	}
	return ""
}
