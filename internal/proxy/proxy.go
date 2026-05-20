package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/ankoehn/burrow/pkg/clientip"
)

// AccessChecker enforces the service's access-mode policy before the proxy
// opens a tunnel stream. The concrete implementation is provided by Task 8;
// this task defines the interface and uses a permissive stub in tests.
//
// Return values:
//   - ok=true: allow the request to proceed.
//   - ok=false: write status + body (plain text unless hdr overrides Content-Type)
//   - optional extra headers from hdr, then return without proxying.
type AccessChecker interface {
	Allow(ctx context.Context, res *Resolved, r *http.Request) (ok bool, status int, body string, hdr http.Header)
}

// AIChain is the v0.4.0 middleware chain (internal/aigw.Chain) the proxy
// dispatches into between Access.Allow and ReverseProxy.ServeHTTP, but ONLY
// for services with a non-default service_ai_config blob. When AIChain is
// nil (the default, and the v0.3.0 build), the proxy serves every request
// through the v0.3.0 pass-through path.
//
// Interface — rather than a direct dependency on *aigw.Chain — to keep the
// proxy → aigw direction one-way (aigw never imports proxy). The proxy
// hands the chain its own Resolved metadata plus the downstream
// ReverseProxy handler; the chain calls back into the proxy via that
// handler when it decides to forward upstream.
//
// The chain itself is responsible for resolving + decoding the AI config
// blob (so the proxy never needs to depend on JSON shape). A nil-zero
// config means: pass through to proxyHandler unchanged.
type AIChain interface {
	// Dispatch runs the chain for one request. serviceID is the resolved
	// service identity; localHost / apiKeyHeader come from the Resolved
	// metadata. proxyHandler is the v0.3.0 ReverseProxy handler the chain
	// delegates to on a cache MISS / no short-circuit. The chain MUST
	// either short-circuit (writing status + body to w) OR forward via
	// proxyHandler — never both.
	Dispatch(w http.ResponseWriter, r *http.Request,
		serviceID, localHost, apiKeyHeader string,
		proxyHandler http.Handler)
}

// Proxy is the vhost reverse proxy. It satisfies http.Handler and must be
// registered on the ingress listener (the TLS front-door, not the API port).
type Proxy struct {
	dialer         StreamDialer
	checker        AccessChecker
	authDomain     string
	gate           http.Handler // /__burrow/* gate; nil → 404 until Task 9
	log            *slog.Logger
	trustedProxies []*net.IPNet
	ingressPort    string // optional; included in X-Forwarded-Port when non-empty

	// v0.4.0 AI middleware chain; nil → v0.3.0 pass-through for every request.
	// Wired via WithAIChain. The chain itself resolves whether a given
	// service has an AI config blob — proxy never decodes JSON.
	aiChain AIChain
}

// New constructs a Proxy.
//
//   - d:          StreamDialer used to look up and connect to tunnels.
//   - ac:         AccessChecker enforcing per-service access mode policy.
//   - authDomain: base domain, e.g. "tunnels.example.com". Requests with host
//     exactly equal to authDomain and path starting "/__burrow/" are routed to
//     the gate handler.
//   - log:        structured logger; must not be nil.
//
// Optional configuration is set via functional options; see WithGate,
// WithTrustedProxies, WithIngressPort.
func New(d StreamDialer, ac AccessChecker, authDomain string, log *slog.Logger, opts ...Option) *Proxy {
	p := &Proxy{
		dialer:     d,
		checker:    ac,
		authDomain: authDomain,
		log:        log,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Option is a functional option for Proxy.
type Option func(*Proxy)

// WithGate registers the gate handler that serves /__burrow/* on the auth
// domain. Task 9 calls this when wiring the real gate. When nil (default) the
// proxy responds 404 for those paths.
func WithGate(h http.Handler) Option {
	return func(p *Proxy) { p.gate = h }
}

// WithTrustedProxies sets the CIDRs whose X-Forwarded-For headers are honored
// when building the authoritative X-Forwarded-For header sent upstream.
// When empty (the default), the raw TCP peer address is always used.
func WithTrustedProxies(cidrs []*net.IPNet) Option {
	return func(p *Proxy) { p.trustedProxies = cidrs }
}

// WithIngressPort sets the port included in the X-Forwarded-Port header sent
// upstream. When empty (the default) the header is omitted.
func WithIngressPort(port string) Option {
	return func(p *Proxy) { p.ingressPort = port }
}

// WithAIChain registers the v0.4.0 middleware chain. When set, the proxy
// dispatches every successful request (post-access-check) through the chain
// before invoking the ReverseProxy. The chain itself decides whether a
// service has an AI config and, if not, calls back into the supplied
// proxyHandler unchanged — byte-for-byte preserving v0.3.0 behaviour.
//
// When nil (the default), the proxy serves every request via the v0.3.0
// pass-through path with no chain overhead.
func WithAIChain(c AIChain) Option {
	return func(p *Proxy) { p.aiChain = c }
}

// ServeHTTP implements http.Handler.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Strip optional port from Host.
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	// h2c upgrade: not supported.
	// Respond 505 HTTP Version Not Supported. h2c (cleartext HTTP/2 upgrade)
	// is out of scope for Burrow's ingress (see package-level doc).
	if strings.EqualFold(r.Header.Get("Upgrade"), "h2c") {
		http.Error(w, "h2c not supported", http.StatusHTTPVersionNotSupported)
		return
	}

	// Requests to the auth domain itself under /__burrow/* go to the gate.
	if host == p.authDomain && strings.HasPrefix(r.URL.Path, "/__burrow/") {
		if p.gate != nil {
			p.gate.ServeHTTP(w, r)
		} else {
			http.NotFound(w, r)
		}
		return
	}

	// Expect <label>.<authDomain>.
	suffix := "." + p.authDomain
	if !strings.HasSuffix(host, suffix) {
		p.notFound(w)
		return
	}
	label := strings.TrimSuffix(host, suffix)
	if label == "" || strings.Contains(label, ".") {
		// Multi-level subdomain or empty label: not a direct tunnel label.
		p.notFound(w)
		return
	}

	ctx := r.Context()

	// Step 1: look up service metadata (no stream opened yet).
	res, err := p.dialer.Lookup(ctx, label)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			p.notFound(w)
			return
		}
		p.log.Warn("proxy lookup error", "subdomain", label, "err", err)
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}

	// Step 2: enforce access mode BEFORE opening a stream.
	ok, status, body, hdr := p.checker.Allow(ctx, res, r)
	if !ok {
		for k, vs := range hdr {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		}
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
		return
	}

	// Determine upstream Host value.
	upstreamHost := res.LocalHost
	if upstreamHost == "" {
		upstreamHost = label + suffix // fallback: use the public vhost
	}

	// Resolve authoritative client IP once, using the inbound request.
	// This is done here (before the Rewrite func) so we capture the original
	// request's RemoteAddr and headers. In the Rewrite closure, r.In holds the
	// original, but we capture clientIP here for clarity and to avoid any
	// ambiguity about which request object is used.
	resolvedClientIP := clientip.Resolve(
		r.RemoteAddr,
		r.Header.Get("X-Forwarded-For"),
		r.Header.Get("X-Real-IP"),
		p.trustedProxies,
	)

	// Capture values needed by Rewrite as local vars.
	authDomain := p.authDomain
	ingressPort := p.ingressPort

	// Step 3: build a per-request ReverseProxy.
	//
	// We use the Rewrite API (Go 1.20+) instead of Director. When Rewrite is
	// used, httputil.ReverseProxy removes Forwarded, X-Forwarded-For,
	// X-Forwarded-Host, X-Forwarded-Proto (stdlib behavior); we explicitly
	// Del X-Forwarded-Port below since stdlib does not auto-strip it.
	// Hop-by-hop headers are also removed by the stdlib.
	//
	// FlushInterval=-1: disable internal buffering for SSE / chunked streaming.
	// This is mandatory — LLM token streams and SSE must flush immediately.
	rp := &httputil.ReverseProxy{
		FlushInterval: -1,

		Rewrite: func(pr *httputil.ProxyRequest) {
			// Stdlib has stripped Forwarded, X-Forwarded-For, X-Forwarded-Host,
			// and X-Forwarded-Proto from pr.Out before this function is called.
			// X-Forwarded-Port is NOT auto-stripped by stdlib, so we delete it
			// unconditionally here — Burrow is the trust boundary and a visitor
			// must never be able to inject an arbitrary port value upstream.
			pr.Out.Header.Del("X-Forwarded-Port")

			// Set Burrow's authoritative forwarding values.
			pr.Out.Header.Set("X-Forwarded-For", resolvedClientIP)
			pr.Out.Header.Set("X-Forwarded-Proto", "https")
			pr.Out.Header.Set("X-Forwarded-Host", label+"."+authDomain)
			if ingressPort != "" {
				pr.Out.Header.Set("X-Forwarded-Port", ingressPort)
			}

			// Rewrite destination.
			pr.Out.URL = &url.URL{
				Scheme:   "http",
				Host:     upstreamHost,
				Path:     pr.In.URL.Path,
				RawQuery: pr.In.URL.RawQuery,
			}
			pr.Out.Host = upstreamHost
		},

		Transport: &http.Transport{
			// DialContext ignores the addr argument: we always connect to the
			// tunnel stream for this label. The Transport calls this for each
			// new connection (i.e. each proxied request, since we do not pool).
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				conn, err := p.dialer.DialTunnelStream(ctx, label)
				if err != nil {
					return nil, err
				}
				return conn, nil
			},
			DisableCompression: true,
			// HTTP/2 to upstream is not applicable: tunnels speak HTTP/1.1
			// to the local server they wrap. Disable to avoid surprises.
			ForceAttemptHTTP2: false,
		},

		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			p.log.Warn("proxy upstream error", "subdomain", label, "err", err)
			if errors.Is(err, ErrNotFound) {
				p.notFound(w)
				return
			}
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
	}

	p.log.Debug("proxy request", "subdomain", label, "method", r.Method, "path", r.URL.Path)

	// v0.4.0: if an AI chain is wired, dispatch through it. The chain itself
	// short-circuits to the v0.3.0 pass-through path (i.e. calls
	// rp.ServeHTTP unchanged) when no AI config exists for this service —
	// so a wired-but-unconfigured chain is byte-for-byte identical to
	// v0.3.0.
	if p.aiChain != nil {
		p.aiChain.Dispatch(w, r,
			res.ServiceID, res.LocalHost, res.APIKeyHeader, rp)
		return
	}

	rp.ServeHTTP(w, r)
}

// notFound writes the canonical 404 for this package: text/plain "tunnel not
// found" with no JSON envelope (this is the data plane, not the management API).
func (p *Proxy) notFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = fmt.Fprint(w, "tunnel not found")
}
