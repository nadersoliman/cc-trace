# Debug Export Config + Secret Redaction Design

**Issue:** #31
**Origin:** [feat(debug): surface OTel export config + outcomes via CC_TRACE_DEBUG, with secret redaction](https://github.com/nadersoliman/cc-trace/issues/31)
**Status:** draft
**Depth:** Standard

---

## Problem Frame

When cc-trace silently fails to export spans — wrong endpoint, missing auth headers, TLS failure, transport drop — the user sees `[INFO] Exported N turns in 0.0s` but no spans arrive at the backend. Diagnosing requires injecting an env-dump wrapper around the hook command and chasing layers manually. The existing `CC_TRACE_DEBUG=true` plumbing and `~/.claude/state/cc_trace.log` sink should surface enough to make every reasonable export failure self-evident from the log alone, without leaking auth secrets.

## Scope

- Add `[DEBUG]` log lines at three points: `InitTracer`, `ExportSessionTrace`, `Shutdown`
- Set `otel.SetErrorHandler` to capture async SDK transport errors
- Create a `redact` function to mask sensitive values by key pattern and value pattern
- **No changes** to `[INFO]` level output
- **No** log rotation work

## Key Decisions

### 1. Redact function location: `internal/logging/redact.go`

The redact function is tightly coupled to the logging package and is only called when producing debug log lines. Placing it alongside `logging.go` keeps the dependency graph flat (no new package). The logging package is small (53 lines) and gains a clear second responsibility boundary.

### 2. Shutdown closure signature: keep `func()`, log internally

Today `InitTracer` returns `(func(), error)` and the shutdown closure discards `tp.Shutdown()` errors. Changing the signature to `func() error` would break `InitTracerWithExporter`, `setupTestTracer`, and `handleStop`.

Instead: capture and log the error + duration inside the closure itself, guarded by `sync.Once` to prevent duplicate logs from the `defer shutdown()` safety net in `handleStop`.

```
shutdown := func() {
    once.Do(func() {
        start := time.Now()
        err := tp.Shutdown(context.Background())
        logging.Debug(fmt.Sprintf("Shutdown: duration=%dms err=%v", ...))
    })
}
```

This satisfies the issue's "surface this" requirement without an API change.

### 3. otel.SetErrorHandler placement: `InitTracerWithExporter`

Placed in `InitTracerWithExporter` (not just `InitTracer`) so both production and test paths register the handler. The handler routes errors to `logging.Debug`:

```
otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
    logging.Debug(fmt.Sprintf("OTel SDK error: %v", err))
}))
```

### 4. CLAUDE_ENV_FILE "sourced" heuristic

cc-trace is a Go binary invoked by Claude Code — it cannot know whether Claude Code actually sourced the env file. The heuristic: if `CLAUDE_ENV_FILE` is set and the file exists, report `sourced=true` (Claude Code sets this var and sources the file as part of hook invocation). If the file doesn't exist, report `sourced=false`.

### 5. Span count: computed from input data

Count spans from the structured input before export rather than from the OTel SDK internals:
- 1 session span (if created this invocation)
- N turn spans
- N LLM spans (turns with Model != "")
- sum of tool spans per turn
- Subagent spans are nested and harder to pre-count; omit from the count or add a counter in `createTurnSpans`

This is approximate but sufficient for debug output.

### 6. Header env var parsing

`OTEL_EXPORTER_OTLP_HEADERS` uses format `key1=val1,key2=val2`. Parse into key-value pairs, log keys verbatim, redact each value individually. A dedicated `RedactHeadersEnv` function handles this format.

## Files Changed

| File | Change |
|------|--------|
| `internal/logging/redact.go` (new) | `Redact(value, key)` and `RedactHeadersEnv(raw)` functions |
| `internal/logging/redact_test.go` (new) | Unit tests against all cases from the issue |
| `internal/tracer/tracer.go` | Debug logging in InitTracer, Shutdown error/duration logging, otel.SetErrorHandler |
| `internal/tracer/tracer_test.go` | Tests for debug logging behavior |
| `cmd/cc-trace/main.go` | No changes needed (shutdown closure handles logging internally) |
| `README.md` | Document CC_TRACE_DEBUG richer output and redaction patterns |

## Redaction Rules (from issue)

**Key-based** (case-insensitive substring): `secret`, `token`, `password`, `bearer`, `authorization`, `cookie`, `api-key`, `apikey`, `client-id`, `client-secret`, keys ending in `_KEY`

**Value-based** (regardless of key): JWT pattern (`eyJ` prefix with two `.` separators), hex string >= 32 chars, `Bearer ` prefix

**Output formats:**
- Sensitive: `[REDACTED len=N]`
- Non-sensitive, len > 16: `first8 ... last4 (len=N)`
- Non-sensitive, len <= 16: verbatim

## Test Scenarios

### Redact function
1. `Authorization: Bearer eyJhbGc...` -> `[REDACTED len=200]` (key match)
2. `CF-Access-Client-Secret: 17da284b...` -> `[REDACTED len=64]` (key match)
3. `OTEL_EXPORTER_OTLP_ENDPOINT: https://otel.example.com` -> verbatim (not sensitive)
4. `OTEL_SERVICE_NAME: claude-code-default` -> verbatim (not sensitive, len=19 > 16 so truncated)
5. Non-sensitive key with JWT value -> `[REDACTED len=N]` (value pattern match)
6. Non-sensitive key with long hex value -> `[REDACTED len=N]` (value pattern match)
7. Non-sensitive key with `Bearer xyz` value -> `[REDACTED len=N]` (value pattern match)
8. Key ending in `_KEY` -> `[REDACTED len=N]`
9. Empty value -> verbatim
10. Short non-sensitive value (len <= 16) -> verbatim

### RedactHeadersEnv
1. `CF-Access-Client-Id=foo,CF-Access-Client-Secret=bar` -> keys visible, values redacted
2. Empty string -> "(absent)"
3. Single key-value pair -> correct parse
4. Malformed (no `=`) -> handled gracefully

### InitTracer debug logging
1. With all env vars set -> all logged with correct present/absent status
2. With no env vars set -> all show "(absent)", endpoint shows default
3. With HTTPS endpoint -> `insecure=false (scheme=https)`
4. With HTTP endpoint -> `insecure=true (scheme=http)`
5. With CLAUDE_ENV_FILE set, file exists -> `file exists=true, sourced=true`
6. With CLAUDE_ENV_FILE set, file missing -> `file exists=false, sourced=false`

### Shutdown logging
1. Successful shutdown -> `duration=Nms err=<nil>`
2. Double shutdown call (defer + explicit) -> logged only once (sync.Once)

### otel.SetErrorHandler
1. SDK error handler is registered and routes to logging.Debug

## Risk

- **Low:** All new code is gated behind `CC_TRACE_DEBUG=true` via existing `logging.Debug()`. Zero overhead when debug is off.
- **Low:** `sync.Once` in shutdown closure prevents double-log but also means the deferred safety-net call won't log if the explicit call already ran. Acceptable — the first call's log is the one that matters.
- **Medium:** The `otel.SetErrorHandler` is global (per the OTel SDK). In tests using `InitTracerWithExporter`, this could interfere if tests run in parallel. Mitigated by the fact that tracer tests already use a global TracerProvider and are not parallelized.
