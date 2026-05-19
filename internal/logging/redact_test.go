package logging

import (
	"strings"
	"testing"
)

func TestRedact_SensitiveKeys(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{"authorization header", "Authorization", "Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc123signature", "[REDACTED len=71]"},
		{"client-secret", "CF-Access-Client-Secret", "17da284b1e2f4a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b", "[REDACTED len=64]"},
		{"token key", "X-Auth-Token", "mytoken123", "[REDACTED len=10]"},
		{"password key", "DB_PASSWORD", "hunter2", "[REDACTED len=7]"},
		{"cookie key", "Session-Cookie", "abc123", "[REDACTED len=6]"},
		{"apikey key", "X-ApiKey", "key12345", "[REDACTED len=8]"},
		{"api-key key", "X-Api-Key", "key12345", "[REDACTED len=8]"},
		{"client-id key", "CF-Access-Client-Id", "some-client-id-value-here-long", "[REDACTED len=30]"},
		{"key ending in _KEY", "OTEL_EXPORTER_OTLP_CLIENT_KEY", "/path/to/client.key", "[REDACTED len=19]"},
		{"case insensitive", "x-authorization", "val", "[REDACTED len=3]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.value, tt.key)
			if got != tt.want {
				t.Errorf("Redact(%q, %q) = %q, want %q", tt.value, tt.key, got, tt.want)
			}
		})
	}
}

func TestRedact_SensitiveValues(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{"JWT value", "X-Custom", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc123signature"},
		{"Bearer prefix value", "X-Custom", "Bearer some-token-here"},
		{"long hex value", "X-Custom", "17da284b1e2f4a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a2b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.value, tt.key)
			if !strings.HasPrefix(got, "[REDACTED len=") {
				t.Errorf("Redact(%q, %q) = %q, want [REDACTED ...]", tt.value, tt.key, got)
			}
		})
	}
}

func TestRedact_NonSensitive(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{"empty value", "OTEL_SERVICE_NAME", "", ""},
		{"short value", "OTEL_SERVICE_NAME", "my-service", "my-service"},
		{"exactly 16 chars", "OTEL_SERVICE_NAME", "1234567890123456", "1234567890123456"},
		{"long URL", "OTEL_EXPORTER_OTLP_ENDPOINT", "https://otel.example.com", "https:// ... .com (len=24)"},
		{"long service name", "OTEL_SERVICE_NAME", "claude-code-default", "claude-c ... ault (len=19)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.value, tt.key)
			if got != tt.want {
				t.Errorf("Redact(%q, %q) = %q, want %q", tt.value, tt.key, got, tt.want)
			}
		})
	}
}

func TestRedactHeadersEnv(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"absent", "", "(absent)"},
		{"single non-sensitive pair", "Content-Type=application/json", "[1 keys: Content-Type=application/json]"},
		{"two sensitive pairs", "CF-Access-Client-Id=abc123,CF-Access-Client-Secret=secret456", "[2 keys: CF-Access-Client-Id=[REDACTED len=6], CF-Access-Client-Secret=[REDACTED len=9]]"},
		{"malformed no equals", "garbage", "[1 keys: garbage]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactHeadersEnv(tt.raw)
			if got != tt.want {
				t.Errorf("RedactHeadersEnv(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
