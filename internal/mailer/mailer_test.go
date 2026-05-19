package mailer

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestConfigValidate(t *testing.T) {
	cases := []struct {
		name string
		c    Config
		ok   bool
	}{
		{"ok", Config{Host: "h", Port: 25, From: "a@b.c", TLS: TLSNone}, true},
		{"no host", Config{Port: 25, From: "a@b.c", TLS: TLSNone}, false},
		{"bad port", Config{Host: "h", Port: 0, From: "a@b.c", TLS: TLSNone}, false},
		{"no from", Config{Host: "h", Port: 25, TLS: TLSNone}, false},
		{"bad tls", Config{Host: "h", Port: 25, From: "a@b.c", TLS: "weird"}, false},
	}
	for _, c := range cases {
		err := c.c.Validate()
		if (err == nil) != c.ok {
			t.Errorf("%s: Validate err=%v want ok=%v", c.name, err, c.ok)
		}
	}
}

// fakeSMTP runs a one-shot plaintext SMTP server. If rejectAuth is true it
// answers AUTH with 535. Returns the listen address.
func fakeSMTP(t *testing.T, rejectAuth bool) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		w := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		w("220 fake ESMTP")
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				w("250-fake")
				w("250 AUTH PLAIN LOGIN")
			case strings.HasPrefix(cmd, "AUTH"):
				if rejectAuth {
					w("535 auth failed")
				} else {
					w("235 ok")
				}
			case strings.HasPrefix(cmd, "MAIL FROM"):
				w("250 ok")
			case strings.HasPrefix(cmd, "RCPT TO"):
				w("250 ok")
			case cmd == "DATA":
				w("354 send it")
				// Consume the message body until a lone "." line; the SMTP
				// server must NOT respond to body lines (only to the
				// terminating dot), or the response stream desyncs.
				for {
					bl, berr := br.ReadString('\n')
					if berr != nil {
						return
					}
					if strings.TrimRight(bl, "\r\n") == "." {
						break
					}
				}
				w("250 queued")
			case cmd == "QUIT":
				w("221 bye")
				return
			default:
				w("250 ok")
			}
		}
	}()
	return ln.Addr().String()
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port := 0
	for _, r := range p {
		port = port*10 + int(r-'0')
	}
	return h, port
}

func TestSendTestHappyPath(t *testing.T) {
	addr := fakeSMTP(t, false)
	host, port := splitHostPort(t, addr)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := SendTest(ctx, Config{
		Host: host, Port: port, Username: "u", Password: "p",
		From: "noreply@example.com", TLS: TLSNone,
	}, "ops@example.com")
	if err != nil {
		t.Fatalf("SendTest happy: %v", err)
	}
}

func TestSendTestAuthRejected(t *testing.T) {
	addr := fakeSMTP(t, true)
	host, port := splitHostPort(t, addr)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := SendTest(ctx, Config{
		Host: host, Port: port, Username: "u", Password: "p",
		From: "noreply@example.com", TLS: TLSNone,
	}, "ops@example.com")
	if err == nil {
		t.Fatal("SendTest must fail when AUTH is rejected")
	}
}
