package devcert

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateWritesVerifiableChain(t *testing.T) {
	dir := t.TempDir()
	if err := Generate(dir, true); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for _, f := range []string{"dev-ca.pem", "dev-ca-key.pem", "dev-server.pem", "dev-server-key.pem"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("missing %s: %v", f, err)
		}
	}
	caPEM, _ := os.ReadFile(filepath.Join(dir, "dev-ca.pem"))
	srvPEM, _ := os.ReadFile(filepath.Join(dir, "dev-server.pem"))
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("append CA failed")
	}
	srv := parseCert(t, srvPEM)
	if _, err := srv.Verify(x509.VerifyOptions{Roots: pool, DNSName: "localhost"}); err != nil {
		t.Fatalf("server cert does not verify against CA for localhost: %v", err)
	}
	if !containsIP(srv.IPAddresses, net.ParseIP("127.0.0.1")) {
		t.Fatal("missing 127.0.0.1 SAN")
	}
}

func TestGenerateIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := Generate(dir, true); err != nil {
		t.Fatal(err)
	}
	b1, _ := os.ReadFile(filepath.Join(dir, "dev-server.pem"))
	if err := Generate(dir, false); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(filepath.Join(dir, "dev-server.pem"))
	if string(b1) != string(b2) {
		t.Fatal("non-force Generate regenerated existing cert")
	}
}

func parseCert(t *testing.T, pemBytes []byte) *x509.Certificate {
	t.Helper()
	blk, _ := pem.Decode(pemBytes)
	if blk == nil {
		t.Fatal("pem decode failed")
	}
	c, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	return c
}

func containsIP(ips []net.IP, want net.IP) bool {
	for _, ip := range ips {
		if ip.Equal(want) {
			return true
		}
	}
	return false
}
