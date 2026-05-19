# Debug Export Config + Secret Redaction Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** When `CC_TRACE_DEBUG=true`, log enough at InitTracer, ExportSessionTrace, and Shutdown to make every reasonable export failure self-evident from `cc_trace.log` alone, without leaking auth secrets.

**Architecture:** New `Redact` / `RedactHeadersEnv` functions in `internal/logging/redact.go`. Debug logging added to `InitTracer` (env var dump), `ExportSessionTrace` (span count), and Shutdown (error + duration). `otel.SetErrorHandler` routes async SDK errors to `logging.Debug`. Shutdown closure logs internally via `sync.Once`.

**Tech Stack:** Go, OTel SDK v1.40.0, `internal/logging` package

---

### Task 1: Create redact function and tests

**Files:**
- Create: `internal/logging/redact.go`
- Create: `internal/logging/redact_test.go`

**Step 1: Write `internal/logging/redact.go`**

```go
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
```

**Step 2: Write `internal/logging/redact_test.go`**

Test cases from the issue:
- `Authorization` key with `Bearer eyJhbGc...` value -> `[REDACTED len=N]`
- `CF-Access-Client-Secret` key with hex value -> `[REDACTED len=N]`
- `OTEL_EXPORTER_OTLP_ENDPOINT` key with URL -> verbatim or truncated (len > 16)
- `OTEL_SERVICE_NAME` key with `claude-code-default` -> truncated (len=19)
- Non-sensitive key with JWT value -> `[REDACTED len=N]` (value pattern)
- Non-sensitive key with `Bearer xxx` value -> `[REDACTED len=N]` (value pattern)
- Key ending in `_KEY` -> `[REDACTED len=N]`
- Empty value -> empty string
- Short non-sensitive value -> verbatim
- `RedactHeadersEnv` with `CF-Access-Client-Id=foo,CF-Access-Client-Secret=bar`
- `RedactHeadersEnv` with empty string -> `"(absent)"`
- `RedactHeadersEnv` with malformed pair (no `=`)

**Step 3: Run tests**

Run: `cd /Users/nadersoliman/projects/cc-trace && go test ./internal/logging/ -run TestRedact -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/logging/redact.go internal/logging/redact_test.go
git commit -m "feat(logging): add Redact and RedactHeadersEnv for secret-safe debug output" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 2: Add debug logging to InitTracer

**Files:**
- Modify: `internal/tracer/tracer.go`

**Step 1: Add `logExportConfig` function**

Add a new function in `tracer.go` that logs each OTel env var with redaction. Called from `InitTracer` after the exporter is created.

Env vars to log (from issue):
- `OTEL_EXPORTER_OTLP_ENDPOINT`
- `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`
- `OTEL_EXPORTER_OTLP_HEADERS`
- `OTEL_EXPORTER_OTLP_TRACES_HEADERS`
- `OTEL_EXPORTER_OTLP_PROTOCOL`
- `OTEL_RESOURCE_ATTRIBUTES`
- `OTEL_SERVICE_NAME`
- `OTEL_EXPORTER_OTLP_CERTIFICATE`
- `OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE`
- `OTEL_EXPORTER_OTLP_CLIENT_KEY`
- `CLAUDE_ENV_FILE`

Log format examples:
```
InitTracer: endpoint=https://otel.example.com/v1/traces insecure=false (scheme=https)
env: OTEL_EXPORTER_OTLP_ENDPOINT=https://o ... .com (len=24, present)
env: OTEL_EXPORTER_OTLP_HEADERS=[2 keys: CF-Access-Client-Id=[REDACTED len=38], CF-Access-Client-Secret=[REDACTED len=64]]
env: CLAUDE_ENV_FILE=/Users ... .sh (len=104, present, file exists=true, sourced=true)
```

For header env vars (`OTEL_EXPORTER_OTLP_HEADERS`, `OTEL_EXPORTER_OTLP_TRACES_HEADERS`), use `logging.RedactHeadersEnv`.

For `CLAUDE_ENV_FILE`: check `os.Stat` for file existence. Report `sourced=true` if file exists (Claude Code sources files it points to via this var), `sourced=false` otherwise.

For all other env vars: use `logging.Redact(value, key)` and report `present`/`absent`.

**Step 2: Call `logExportConfig` from `InitTracer`**

Add the call between the `isInsecureEndpoint()` check and the exporter creation, so the resolved endpoint + insecure state is logged first, then each env var.

**Step 3: Run tests**

Run: `cd /Users/nadersoliman/projects/cc-trace && go test ./internal/tracer/ -v`
Expected: PASS (existing tests don't enable debug logging)

**Step 4: Commit**

```bash
git add internal/tracer/tracer.go
git commit -m "feat(tracer): log OTel export config at InitTracer when debug enabled" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Add otel.SetErrorHandler and Shutdown logging

**Files:**
- Modify: `internal/tracer/tracer.go`

**Step 1: Register `otel.SetErrorHandler` in `InitTracerWithExporter`**

Add before `otel.SetTracerProvider(tp)`:

```go
otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
    logging.Debug(fmt.Sprintf("OTel SDK error (via otel.SetErrorHandler): %v", err))
}))
```

Import `otel.ErrorHandlerFunc` — this is the function adapter type in `go.opentelemetry.io/otel` that implements the `ErrorHandler` interface.

**Step 2: Add Shutdown error + duration logging**

Change the shutdown closure in `InitTracerWithExporter` to:

```go
var once sync.Once
shutdown := func() {
    once.Do(func() {
        start := time.Now()
        err := tp.Shutdown(context.Background())
        dur := time.Since(start)
        logging.Debug(fmt.Sprintf("Shutdown: duration=%dms err=%v", dur.Milliseconds(), err))
    })
}
```

Add `"sync"` to imports.

**Step 3: Run tests**

Run: `cd /Users/nadersoliman/projects/cc-trace && go test ./... -v`
Expected: ALL PASS. The `setupTestTracer` helper calls `InitTracerWithExporter` and returns `shutdown` — the sync.Once guard ensures clean behavior.

**Step 4: Commit**

```bash
git add internal/tracer/tracer.go
git commit -m "feat(tracer): log Shutdown error/duration and register OTel SDK error handler" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 4: Add ExportSessionTrace debug logging

**Files:**
- Modify: `internal/tracer/tracer.go`

**Step 1: Add span count computation**

Before the `createTurnSpans` call in `ExportSessionTrace`, compute the expected span count:

```go
spanCount := 0
if sessionSpan != nil {
    spanCount++ // session span created this invocation
}
for _, turn := range turns {
    spanCount++ // turn span
    if turn.Model != "" {
        spanCount++ // LLM span
    }
    spanCount += len(turn.ToolCalls) // tool spans
}
```

**Step 2: Replace the existing debug line**

Change the existing `logging.Debug(fmt.Sprintf("Exported %d turns for session %s", ...))` to:

```go
logging.Debug(fmt.Sprintf("ExportSessionTrace: spans=%d (session=%s, turns=%d)", spanCount, truncate(sessionID, 8), len(turns)))
```

**Step 3: Run tests**

Run: `cd /Users/nadersoliman/projects/cc-trace && go test ./internal/tracer/ -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/tracer/tracer.go
git commit -m "feat(tracer): log span count at ExportSessionTrace when debug enabled" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 5: Update documentation

**Files:**
- Modify: `README.md`

**Step 1: Update CC_TRACE_DEBUG description**

In the environment variables table, expand the `CC_TRACE_DEBUG` description to mention the richer debug output: resolved endpoint, env var presence, header keys (redacted values), transport mode, Shutdown error/duration, and OTel SDK async errors.

**Step 2: Add redaction note**

Add a brief note that values matching sensitive patterns (`secret`, `token`, `bearer`, `authorization`, `cookie`, `api-key`, `client-secret`, keys ending in `_KEY`) are redacted as `[REDACTED len=N]` in debug output.

**Step 3: Commit**

```bash
git add README.md
git commit -m "docs: document CC_TRACE_DEBUG richer output and secret redaction" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 6: Run full verification

**Step 1: Run all tests**

Run: `cd /Users/nadersoliman/projects/cc-trace && go test ./... -v -count=1`
Expected: all tests PASS

**Step 2: Build binary**

Run: `cd /Users/nadersoliman/projects/cc-trace && go build -o /dev/null ./cmd/cc-trace/`
Expected: success

**Step 3: Run vet**

Run: `cd /Users/nadersoliman/projects/cc-trace && go vet ./...`
Expected: no issues

**Step 4: Run gofmt check**

Run: `cd /Users/nadersoliman/projects/cc-trace && gofmt -s -l .`
Expected: no output (all files formatted)
