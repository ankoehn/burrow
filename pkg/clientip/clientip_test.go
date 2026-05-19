package clientip_test

import (
	"net"
	"testing"

	"github.com/ankoehn/burrow/pkg/clientip"
)

func TestBuildCIDRs(t *testing.T) {
	cidrs := clientip.BuildCIDRs([]string{"10.0.0.0/8", "192.168.1.1", "::1", ""})
	if len(cidrs) != 3 {
		t.Fatalf("want 3 CIDRs, got %d", len(cidrs))
	}
}

func TestBuildCIDRs_Invalid(t *testing.T) {
	cidrs := clientip.BuildCIDRs([]string{"not-an-ip", "999.999.999.999/32"})
	if len(cidrs) != 0 {
		t.Fatalf("want 0 CIDRs for invalid inputs, got %d", len(cidrs))
	}
}

func TestPeerIP(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"1.2.3.4:5678", "1.2.3.4"},
		{"[::1]:80", "::1"},
		{"notanaddr", "notanaddr"},
	}
	for _, tc := range tests {
		got := clientip.PeerIP(tc.in)
		if got != tc.want {
			t.Errorf("PeerIP(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestInCIDRList(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	cidrs := []*net.IPNet{cidr}

	if !clientip.InCIDRList("10.1.2.3", cidrs) {
		t.Error("10.1.2.3 should be in 10.0.0.0/8")
	}
	if clientip.InCIDRList("192.168.1.1", cidrs) {
		t.Error("192.168.1.1 should not be in 10.0.0.0/8")
	}
	if clientip.InCIDRList("not-an-ip", cidrs) {
		t.Error("not-an-ip should not match")
	}
}

func TestFromXFF(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"1.2.3.4", "1.2.3.4"},
		{"1.2.3.4, 10.0.0.1", "1.2.3.4"},
		{"  5.6.7.8 , 10.0.0.1", "5.6.7.8"},
	}
	for _, tc := range tests {
		got := clientip.FromXFF(tc.in)
		if got != tc.want {
			t.Errorf("FromXFF(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolve_NoTrustedProxies(t *testing.T) {
	// When trusted list is empty, always use peer IP regardless of headers.
	got := clientip.Resolve("1.2.3.4:5678", "9.9.9.9", "", nil)
	if got != "1.2.3.4" {
		t.Errorf("want 1.2.3.4, got %q", got)
	}
}

func TestResolve_UntrustedPeer(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	cidrs := []*net.IPNet{cidr}
	// Peer not in trusted range → ignore forwarded header.
	got := clientip.Resolve("5.5.5.5:1234", "9.9.9.9", "", cidrs)
	if got != "5.5.5.5" {
		t.Errorf("want 5.5.5.5, got %q", got)
	}
}

func TestResolve_TrustedPeer_XFF(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	cidrs := []*net.IPNet{cidr}
	got := clientip.Resolve("10.0.0.1:1234", "1.2.3.4, 10.0.0.1", "", cidrs)
	if got != "1.2.3.4" {
		t.Errorf("want 1.2.3.4, got %q", got)
	}
}

func TestResolve_TrustedPeer_XRealIP(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	cidrs := []*net.IPNet{cidr}
	got := clientip.Resolve("10.0.0.1:1234", "", "1.2.3.4", cidrs)
	if got != "1.2.3.4" {
		t.Errorf("want 1.2.3.4, got %q", got)
	}
}

func TestResolve_TrustedPeer_NoForwarded(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	cidrs := []*net.IPNet{cidr}
	got := clientip.Resolve("10.0.0.1:1234", "", "", cidrs)
	if got != "10.0.0.1" {
		t.Errorf("want 10.0.0.1, got %q", got)
	}
}
