package redact

import (
	"bytes"
	"log"
	"testing"
)

func TestDiagnosticPrivacy(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{"plain diagnostic", "dial example.com:443 failed: timeout", "dial example.com:443 failed: timeout"},
		{"listener", "[listen] mixed://canary-user:canary-pass@0.0.0.0:6666", "[listen] mixed://0.0.0.0:6666"},
		{"subscription", `Get "https://canary-user:canary-pass@example.com/private-path?token=canary-token#private-fragment": timeout`, `Get "https://example.com": timeout`},
		{"IPv6", "socks5://canary-user:canary-pass@[::1]:6666", "socks5://[::1]:6666"},
		{"encoded VMess", "node vmess://encoded-private-node", "node vmess://[redacted]"},
		{"encoded Shadowsocks", "node ss://encoded-private-node", "node ss://[redacted]"},
		{"Shadowsocks URI", "ss://method:canary-pass@example.com:443", "ss://[redacted]"},
		{"chain", "http://canary-user:canary-pass@example.com:80,ss://encoded-private-node", "http://[redacted]"},
		{"malformed URL", "https://canary-user:canary-pass@bad%host/private-path", "https://[redacted]"},
		{"query with apostrophe", "Get https://example.com/private'path?token=canary-token", "Get https://example.com"},
		{"quoted malformed credentials", `parse "mixed://canary-user:canary pass@example.com:6666": invalid userinfo`, `parse "mixed://[redacted]": invalid userinfo`},
		{"escaped quoted URL", `Get "https://example.com/private\"path?token=canary-token": timeout`, `Get "https://example.com": timeout`},
		{"glider authentication failure", "[http] auth failed from 127.0.0.1, auth info: canary-user:canary-pass", "[http] auth failed from 127.0.0.1, auth info: [redacted]"},
		{"authorization headers", "Proxy-Authorization: Basic canary-encoded-auth\r\nAuthorization: Bearer canary-token\r\nnext message", "Proxy-Authorization: [redacted]\r\nAuthorization: [redacted]\r\nnext message"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Text(tt.input); got != tt.want {
				t.Fatalf("sanitized diagnostic = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStandardLoggerSanitizesCredentials(t *testing.T) {
	var out bytes.Buffer
	l := log.New(Writer{Writer: &out}, "", 0)
	l.Printf("[http] auth failed, auth info: %s:%s", "canary-user", "canary-pass")
	if got, want := out.String(), "[http] auth failed, auth info: [redacted]\n"; got != want {
		t.Fatalf("standard logger output = %q, want %q", got, want)
	}
}
