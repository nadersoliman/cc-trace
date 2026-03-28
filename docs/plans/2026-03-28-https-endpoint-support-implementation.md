# HTTPS Endpoint Support Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Support HTTPS OTLP endpoints by removing the hardcoded `WithInsecure()` and applying it only for `http://` schemes.

**Architecture:** Extract scheme detection into a testable helper (`isInsecureEndpoint`), use it in `InitTracer` to conditionally apply `WithInsecure()`. The OTel SDK default is HTTPS, so omitting `WithInsecure()` enables TLS automatically. The SDK already reads `OTEL_EXPORTER_OTLP_HEADERS` for auth headers.

**Tech Stack:** Go, OTel SDK (`go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`)

---

### Task 1: Write failing tests for scheme detection

**Files:**
- Modify: `internal/tracer/tracer_test.go`

**Step 1: Write the test**

Add at the end of `internal/tracer/tracer_test.go`:

```go
func TestIsInsecureEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		traces   string
		want     bool
	}{
		{"http explicit", "http://localhost:4318", "", true},
		{"https explicit", "https://otel.example.com", "", true},
		{"empty defaults to insecure", "", "", true},
		{"traces endpoint http", "", "http://localhost:4318/v1/traces", true},
		{"traces endpoint https", "", "https://otel.example.com/v1/traces", false},
		{"endpoint takes precedence", "https://otel.example.com", "http://localhost:4318/v1/traces", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", tt.endpoint)
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", tt.traces)
			if got := isInsecureEndpoint(); got != tt.want {
				t.Errorf("isInsecureEndpoint() = %v, want %v (endpoint=%q traces=%q)", got, tt.want, tt.endpoint, tt.traces)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/nadersoliman/projects/cc-trace && go test ./internal/tracer/ -run TestIsInsecureEndpoint -v 2>&1 | head -5`
Expected: FAIL — `isInsecureEndpoint` undefined

**Step 3: Commit**

```bash
git add internal/tracer/tracer_test.go
git commit -m "test: add failing tests for HTTPS endpoint detection" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 2: Implement isInsecureEndpoint and update InitTracer

**Files:**
- Modify: `internal/tracer/tracer.go:42-55`

**Step 1: Add the isInsecureEndpoint helper**

Add after the `traceIDFromSession` function (after line 34), before `InitTracer`:

```go
// isInsecureEndpoint checks the configured OTLP endpoint scheme.
// Returns true if the endpoint uses http:// (or is unset, defaulting to http://localhost:4318).
// Returns false for https:// endpoints, allowing TLS to be used.
func isInsecureEndpoint() bool {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
	}
	if endpoint == "" {
		return true // no endpoint configured, default is http://localhost:4318
	}
	return !strings.HasPrefix(endpoint, "https://")
}
```

**Step 2: Update InitTracer to use conditional insecure**

Replace the current `InitTracer` function body:

```go
func InitTracer() (func(), error) {
	ctx := context.Background()

	opts := []otlptracehttp.Option{
		otlptracehttp.WithTimeout(60 * time.Second),
	}
	if isInsecureEndpoint() {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create exporter: %w", err)
	}

	shutdown, _, err := InitTracerWithExporter(exporter)
	return shutdown, err
}
```

**Step 3: Run the scheme detection tests**

Run: `cd /Users/nadersoliman/projects/cc-trace && go test ./internal/tracer/ -run TestIsInsecureEndpoint -v`
Expected: all 6 subtests PASS

**Step 4: Run full test suite**

Run: `cd /Users/nadersoliman/projects/cc-trace && go test ./... -v`
Expected: ALL tests PASS

**Step 5: Commit**

```bash
git add internal/tracer/tracer.go
git commit -m "feat: support HTTPS endpoints by conditional WithInsecure" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Update documentation

**Files:**
- Modify: `CLAUDE.md`
- Modify: `README.md`

**Step 1: Update CLAUDE.md env var table**

In the `OTEL_EXPORTER_OTLP_ENDPOINT` row, update the Purpose from:

> Base OTLP endpoint. The HTTP exporter appends `/v1/traces` automatically. Shared with Claude Code's metrics/logs -- no separate traces endpoint needed.

to:

> Base OTLP endpoint. Supports both `http://` and `https://` schemes. The HTTP exporter appends `/v1/traces` automatically. Shared with Claude Code's metrics/logs -- no separate traces endpoint needed.

Update the Note at the bottom of the env var section from:

> **Note:** The hook previously used gRPC (`otlptracegrpc`, port 4317) with a separate `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`. It now uses HTTP/protobuf (`otlptracehttp`) to share the same `OTEL_EXPORTER_OTLP_ENDPOINT` as Claude Code's metrics and logs. Per the OTel spec, the HTTP exporter appends `/v1/traces` to the base endpoint automatically.

to:

> **Note:** The hook previously used gRPC (`otlptracegrpc`, port 4317) with a separate `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`. It now uses HTTP/protobuf (`otlptracehttp`) to share the same `OTEL_EXPORTER_OTLP_ENDPOINT` as Claude Code's metrics and logs. Per the OTel spec, the HTTP exporter appends `/v1/traces` to the base endpoint automatically. TLS is enabled automatically for `https://` endpoints; set `OTEL_EXPORTER_OTLP_HEADERS` for auth headers.

**Step 2: Update README.md env var table**

In the `OTEL_EXPORTER_OTLP_ENDPOINT` row, update the Purpose from:

> OTLP/HTTP endpoint (appends `/v1/traces`)

to:

> OTLP endpoint — `http://` or `https://` (appends `/v1/traces`)

Add `OTEL_EXPORTER_OTLP_HEADERS` to the env var table after `OTEL_RESOURCE_ATTRIBUTES`:

| Variable | Default | Purpose |
|----------|---------|---------|
| `OTEL_EXPORTER_OTLP_HEADERS` | — | Comma-separated `key=value` headers for OTLP requests (e.g. `Authorization=Bearer xxx`). Read automatically by the OTel SDK |

**Step 3: Commit**

```bash
git add CLAUDE.md README.md
git commit -m "docs: document HTTPS endpoint support and auth headers" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 4: Run full verification

**Step 1: Run all tests**

Run: `cd /Users/nadersoliman/projects/cc-trace && go test ./... -v -count=1`
Expected: all tests PASS

**Step 2: Build binary**

Run: `cd /Users/nadersoliman/projects/cc-trace && go build -o /dev/null ./cmd/cc-trace/`
Expected: success

**Step 3: Run vet**

Run: `cd /Users/nadersoliman/projects/cc-trace && go vet ./...`
Expected: no issues
