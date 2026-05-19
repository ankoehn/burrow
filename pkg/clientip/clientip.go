// Package clientip provides trusted-proxy-aware client IP resolution.
//
// It is extracted from internal/api/trusted_proxy.go so that both the API
// layer and the vhost reverse proxy (internal/proxy) can share identical
// client-IP logic without a cross-package import cycle.
//
// Trust model: forwarded headers are honored ONLY when the immediate TCP peer
// is within the caller-supplied trusted-CIDR list. When the list is empty
// (the safe default) the raw peer address is always returned, preventing
// X-Forwarded-For spoofing.
package clientip

import (
	"net"
	"strings"
)

// BuildCIDRs parses a list of CIDR/IP strings into []*net.IPNet.
// Bare IPs (no mask) are converted to host-only CIDRs (/32 or /128).
// Entries that fail to parse are silently skipped.
func BuildCIDRs(proxies []string) []*net.IPNet {
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

// PeerIP extracts the host part from remoteAddr (which is always host:port for
// TCP connections). Returns the raw string unchanged on parse failure.
func PeerIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// InCIDRList reports whether ipStr is contained in any of the given CIDRs.
func InCIDRList(ipStr string, cidrs []*net.IPNet) bool {
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

// FromXFF extracts the leftmost address from X-Forwarded-For, mirroring
// chi's middleware.RealIP logic. Returns "" if no usable value is present.
func FromXFF(xff string) string {
	if xff == "" {
		return ""
	}
	if idx := strings.IndexByte(xff, ','); idx != -1 {
		xff = xff[:idx]
	}
	return strings.TrimSpace(xff)
}

// Resolve returns the effective client IP for a request.
//
//   - remoteAddr is the raw TCP peer address (host:port).
//   - xff is the value of the X-Forwarded-For header (may be "").
//   - xRealIP is the value of the X-Real-IP header (may be "").
//   - trusted is the list of CIDRs whose forwarded headers are honored.
//
// When trusted is empty, remoteAddr's host part is always returned.
// When the peer is in a trusted CIDR, the leftmost XFF entry (or X-Real-IP)
// is returned when present; otherwise the peer's own IP is returned.
func Resolve(remoteAddr, xff, xRealIP string, trusted []*net.IPNet) string {
	peer := PeerIP(remoteAddr)
	if len(trusted) == 0 {
		return peer
	}
	if !InCIDRList(peer, trusted) {
		return peer
	}
	if v := FromXFF(xff); v != "" {
		return v
	}
	if v := strings.TrimSpace(xRealIP); v != "" {
		return v
	}
	return peer
}
