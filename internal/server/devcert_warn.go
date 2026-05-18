package server

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DevCertWarning inspects the PEM certificate file at certPath and reports
// whether it looks like a development / self-signed certificate, together with
// a human-readable reason.  It returns (false, "") for production-looking
// certs and never returns an error: a parse failure is treated as "not dev"
// so that misconfigured-but-intentional setups are not silently swallowed.
//
// Detection criteria (any one is sufficient):
//  1. The configured path contains "certs/dev-" or equals the default dev path.
//  2. The leaf certificate is self-signed: Issuer == Subject (raw bytes match).
//  3. The leaf certificate Subject.CommonName is "localhost".
func DevCertWarning(certPath string) (isDev bool, reason string) {
	// 1. Path heuristic — fast, no I/O.
	cleanPath := filepath.ToSlash(certPath)
	if strings.Contains(cleanPath, "certs/dev-") {
		return true, fmt.Sprintf("cert path matches dev prefix (certs/dev-*): %s", certPath)
	}

	// 2 & 3. Parse the leaf cert from the PEM file.
	pemData, err := os.ReadFile(certPath)
	if err != nil {
		// Can't read — let the TLS stack surface the real error; not our problem here.
		return false, ""
	}
	leaf := firstCertificate(pemData)
	if leaf == nil {
		return false, ""
	}

	if bytes.Equal(leaf.RawIssuer, leaf.RawSubject) {
		return true, fmt.Sprintf("certificate is self-signed (Issuer == Subject: %s)", leaf.Subject.String())
	}

	if leaf.Subject.CommonName == "localhost" {
		return true, fmt.Sprintf("certificate CN is %q", leaf.Subject.CommonName)
	}

	return false, ""
}

// firstCertificate decodes and parses the first CERTIFICATE PEM block found
// in pemData. Returns nil if none is found or parsing fails.
func firstCertificate(pemData []byte) *x509.Certificate {
	for {
		var blk *pem.Block
		blk, pemData = pem.Decode(pemData)
		if blk == nil {
			return nil
		}
		if blk.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			return nil
		}
		return cert
	}
}
