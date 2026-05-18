package api

import (
	"net"
	"net/http"
	"strings"
)

// buildCIDRs parses a list of CIDR/IP strings into []*net.IPNet.
// Bare IPs (no mask) are converted to host-only CIDRs (/32 or /128).
// Entries that fail to parse are silently skipped; config validation in
// internal/config ensures all entries are valid before this is called.
func buildCIDRs(proxies []string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(proxies))
	for _, p := range proxies {
		if p == "" {
			continue
		}
		// Try CIDR first.
		if _, cidr, err := net.ParseCIDR(p); err == nil {
			nets = append(nets, cidr)
			continue
		}
		// Bare IP: synthesize a host CIDR.
		if ip := net.ParseIP(p); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			ip4 := ip.To4()
			if ip4 != nil {
				ip = ip4
			}
			nets = append(nets, &net.IPNet{IP: ip.Mask(net.CIDRMask(bits, bits)), Mask: net.CIDRMask(bits, bits)})
		}
	}
	return nets
}

// peerIP extracts the host part from r.RemoteAddr (which is always host:port
// for TCP connections). Returns the raw string unchanged on parse failure.
func peerIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// inCIDRList reports whether ip is contained in any of the given CIDRs.
func inCIDRList(ipStr string, cidrs []*net.IPNet) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, cidr := range cidrs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// realIPFromHeaders extracts the leftmost address from X-Forwarded-For (or
// X-Real-IP if XFF is absent), mirroring chi's middleware.RealIP logic.
// Returns "" if no usable header is present.
func realIPFromHeaders(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// XFF is a comma-separated list; the leftmost entry is the original client.
		if idx := strings.IndexByte(xff, ','); idx != -1 {
			xff = xff[:idx]
		}
		xff = strings.TrimSpace(xff)
		if xff != "" {
			return xff
		}
	}
	return strings.TrimSpace(r.Header.Get("X-Real-IP"))
}

// TrustedProxyMiddleware returns an HTTP middleware that sets r.RemoteAddr to
// the forwarded client IP only when the immediate TCP peer is within a trusted
// CIDR.
//
//   - If proxies is empty: forwarded headers are NEVER honored; the raw TCP peer
//     is always used. This is the safe default for direct-internet deployments
//     and prevents X-Forwarded-For spoofing from bypassing the per-IP login
//     rate-limiter or poisoning session.ip records.
//   - If proxies is non-empty: X-Forwarded-For / X-Real-IP is honored only when
//     the direct TCP peer IP is in the trusted list; otherwise the raw peer is
//     kept. r.RemoteAddr is rewritten to "<forwardedIP>:0" so that downstream
//     consumers (httprate.KeyByIP, Login) see the correct client address in the
//     same host:port format Go's net package expects.
//
// This middleware MUST run before the per-IP rate-limiter and Login handler.
func TrustedProxyMiddleware(proxies []string) func(http.Handler) http.Handler {
	cidrs := buildCIDRs(proxies)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(cidrs) > 0 {
				peer := peerIP(r.RemoteAddr)
				if inCIDRList(peer, cidrs) {
					if forwarded := realIPFromHeaders(r); forwarded != "" {
						// Rewrite RemoteAddr keeping the host:port shape so
						// net.SplitHostPort works downstream. Port 0 signals that
						// the port is unknown (forwarded headers don't carry it).
						r = r.WithContext(r.Context()) // shallow clone for safety
						r.RemoteAddr = net.JoinHostPort(forwarded, "0")
					}
				}
			}
			// When cidrs is empty (the default), we reach here unchanged —
			// the raw TCP peer stays in RemoteAddr and no header is read.
			next.ServeHTTP(w, r)
		})
	}
}
