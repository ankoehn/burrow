// Package mailer sends Burrow's SMTP "test email" using only the standard
// library (net/smtp + crypto/tls). It is intentionally minimal: connect,
// optionally STARTTLS / implicit-TLS, optionally AUTH, send one tiny message.
// The SMTP password is supplied by the caller from BURROW_SMTP_PASSWORD(_FILE)
// and is NEVER persisted.
package mailer

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// TLSMode selects the transport security used to reach the SMTP server.
type TLSMode string

const (
	// TLSNone is plaintext SMTP (test/loopback only).
	TLSNone TLSMode = "none"
	// TLSSTARTTLS upgrades a plaintext connection via STARTTLS.
	TLSSTARTTLS TLSMode = "starttls"
	// TLSImplicit dials TLS directly (SMTPS, typically port 465).
	TLSImplicit TLSMode = "implicit"
)

// Config is the resolved SMTP configuration. Password is injected by the
// caller (never read from / written to the settings table).
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	TLS      TLSMode
}

// Validate checks the required fields and the TLS enum.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Host) == "" {
		return fmt.Errorf("smtp host is required")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("smtp port must be 1..65535")
	}
	if strings.TrimSpace(c.From) == "" {
		return fmt.Errorf("smtp from-address is required")
	}
	switch c.TLS {
	case TLSNone, TLSSTARTTLS, TLSImplicit:
	default:
		return fmt.Errorf("smtp tls must be none, starttls, or implicit")
	}
	return nil
}

// SendTest connects per Config and sends a single short message to `to`. Any
// failure (dial, TLS, AUTH, protocol) is returned so the Settings page can
// surface it. The context bounds the whole exchange.
func SendTest(ctx context.Context, c Config, to string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	deadline := time.Now().Add(20 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	d := net.Dialer{Deadline: deadline}

	var conn net.Conn
	var err error
	if c.TLS == TLSImplicit {
		conn, err = tls.DialWithDialer(&d, "tcp", addr, &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)

	cl, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return fmt.Errorf("smtp greeting: %w", err)
	}
	defer cl.Close()

	if c.TLS == TLSSTARTTLS {
		if err := cl.StartTLS(&tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if c.Username != "" {
		if err := cl.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := cl.Mail(c.From); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := cl.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt to: %w", err)
	}
	wc, err := cl.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	msg := "From: " + c.From + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: Burrow SMTP test\r\n" +
		"\r\n" +
		"This is a Burrow SMTP connection test. If you received it, email is configured correctly.\r\n"
	if _, err := wc.Write([]byte(msg)); err != nil {
		return fmt.Errorf("smtp write body: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("smtp close body: %w", err)
	}
	return cl.Quit()
}
