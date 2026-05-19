package logging

import (
	"fmt"
	"regexp"
	"strings"
)

var sensitiveKeyPatterns = []string{
	"secret", "token", "password", "bearer", "authorization",
	"cookie", "api-key", "apikey", "client-id", "client-secret",
}

var jwtPattern = regexp.MustCompile(`^eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`)
var longHexPattern = regexp.MustCompile(`^[0-9a-fA-F]{32,}$`)

func Redact(value, key string) string {
	if value == "" {
		return value
	}
	if isSensitiveKey(key) || isSensitiveValue(value) {
		return fmt.Sprintf("[REDACTED len=%d]", len(value))
	}
	if len(value) > 16 {
		return fmt.Sprintf("%s ... %s (len=%d)", value[:8], value[len(value)-4:], len(value))
	}
	return value
}

func RedactHeadersEnv(raw string) string {
	if raw == "" {
		return "(absent)"
	}
	pairs := strings.Split(raw, ",")
	var parts []string
	for _, pair := range pairs {
		idx := strings.Index(pair, "=")
		if idx < 0 {
			parts = append(parts, pair)
			continue
		}
		key := pair[:idx]
		val := pair[idx+1:]
		parts = append(parts, fmt.Sprintf("%s=%s", key, Redact(val, key)))
	}
	return fmt.Sprintf("[%d keys: %s]", len(pairs), strings.Join(parts, ", "))
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, p := range sensitiveKeyPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return strings.HasSuffix(lower, "_key")
}

func isSensitiveValue(value string) bool {
	if strings.HasPrefix(value, "Bearer ") {
		return true
	}
	if jwtPattern.MatchString(value) {
		return true
	}
	return longHexPattern.MatchString(value)
}
