package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ankoehn/burrow/internal/devcert"
)

// TestDevCertWarning_DevCert verifies that a devcert-generated server
// certificate (self-signed CA, CN=localhost) is flagged as a dev cert.
func TestDevCertWarning_DevCert(t *testing.T) {
	dir := t.TempDir()
	if err := devcert.Generate(dir, true); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	certPath := filepath.Join(dir, "dev-server.pem")
	isDev, reason := DevCertWarning(certPath)
	if !isDev {
		t.Fatal("expected dev cert to be detected, got isDev=false")
	}
	if reason == "" {
		t.Fatal("expected non-empty reason for dev cert")
	}
	t.Logf("reason: %s", reason)
}

// TestDevCertWarning_DevPath verifies that a cert path matching "certs/dev-*"
// is flagged even without reading the cert content.
func TestDevCertWarning_DevPath(t *testing.T) {
	// Use a non-existent path — the path heuristic must fire before any I/O.
	isDev, reason := DevCertWarning("certs/dev-server.pem")
	if !isDev {
		t.Fatal("expected dev-path cert to be detected")
	}
	if !strings.Contains(reason, "certs/dev-") {
		t.Fatalf("reason should mention 'certs/dev-': %s", reason)
	}
}

// TestDevCertWarning_ProductionCert verifies that a properly signed cert
// (a CA issues a leaf with a non-localhost CN) is NOT flagged as a dev cert.
func TestDevCertWarning_ProductionCert(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "prod-server.pem")
	writeFakeProductionCert(t, certPath)

	isDev, reason := DevCertWarning(certPath)
	if isDev {
		t.Fatalf("expected production cert NOT to be flagged, got isDev=true reason=%q", reason)
	}
}

// writeFakeProductionCert creates a cert signed by a separate CA (Issuer !=
// Subject) with CN="example.com" (not localhost) and writes it to path.
func writeFakeProductionCert(t *testing.T, path string) {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen CA key: %v", err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Fake Production CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create CA: %v", err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("parse CA: %v", err)
	}

	srvKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen srv key: %v", err)
	}
	srvTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"example.com"},
	}
	srvDER, err := x509.CreateCertificate(rand.Reader, srvTmpl, caCert, &srvKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create srv cert: %v", err)
	}

	pemData := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srvDER})
	if err := os.WriteFile(path, pemData, 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
}
