# Batch Span Export Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace synchronous per-span export with batched export to reduce Stop hook latency from 8-12s to <500ms.

**Architecture:** Swap `SimpleSpanProcessor` (`WithSyncer`) for `BatchSpanProcessor` in the OTel TracerProvider. Spans queue in memory on `End()`, flush in one HTTP batch on `Shutdown()`. Add a `ForceFlush` return value from `InitTracerWithExporter` so tests can flush before asserting.

**Tech Stack:** Go, OpenTelemetry SDK (`go.opentelemetry.io/otel/sdk/trace`)

---

### Task 1: Write failing test for batch flush behavior

**Files:**
- Modify: `internal/tracer/tracer_test.go`

**Step 1: Write the failing test**

Add a test that verifies spans are only visible after flushing — this test will fail with the current `SimpleSpanProcessor` because spans are exported synchronously (visible immediately without flush).

```go
func TestBatchFlush_SpansVisibleAfterFlush(t *testing.T) {
	logging.Init(filepath.Join(t.TempDir(), "test.log"), false)
	exporter := tracetest.NewInMemoryExporter()
	shutdown, flush, err := InitTracerWithExporter(exporter)
	if err != nil {
		t.Fatalf("InitTracerWithExporter: %v", err)
	}
	defer shutdown()

	// Create a span
	sessionID := "test-batch-flush"
	now := time.Now()
	turns := []hook.Turn{
		{
			Number:    1,
			Model:     "claude-sonnet-4-20250514",
			StartTime: now,
			EndTime:   now.Add(1 * time.Second),
		},
	}
	ss := &hook.SessionState{}
	ExportSessionTrace(sessionID, turns, nil, nil, ss)

	// Flush must be called to see spans with batch processor
	flush()

	spans := exporter.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("expected 3 spans after flush, got %d", len(spans))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tracer/ -run TestBatchFlush -v`
Expected: FAIL — `InitTracerWithExporter` returns 2 values, not 3

**Step 3: Commit**

```bash
git add internal/tracer/tracer_test.go
git commit -m "test: add failing test for batch flush behavior"
```

---

### Task 2: Switch to BatchSpanProcessor and add ForceFlush return

**Files:**
- Modify: `internal/tracer/tracer.go:53-80`

**Step 1: Update `InitTracerWithExporter` to use BatchSpanProcessor and return flush**

Replace the function body:

```go
// InitTracerWithExporter sets up the OTel TracerProvider with the given exporter.
// This allows tests to inject an in-memory exporter instead of a real OTLP one.
//
// Returns (shutdown, flush, error):
//   - shutdown: flushes pending spans and shuts down the provider
//   - flush: exports pending spans without shutting down (for tests)
func InitTracerWithExporter(exporter sdktrace.SpanExporter) (func(), func(), error) {
	ctx := context.Background()

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithAttributes(
			attribute.String("hook.version", "0.1.0"),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create resource: %w", err)
	}

	bsp := sdktrace.NewBatchSpanProcessor(exporter)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(bsp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tp.Shutdown(ctx)
	}
	flush := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tp.ForceFlush(ctx)
	}
	return shutdown, flush, nil
}
```

**Step 2: Update `InitTracer` to match new signature**

```go
func InitTracer() (func(), error) {
	ctx := context.Background()

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithInsecure(),
		otlptracehttp.WithTimeout(5*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("create exporter: %w", err)
	}

	shutdown, _, err := InitTracerWithExporter(exporter)
	return shutdown, err
}
```

**Step 3: Run the new test**

Run: `go test ./internal/tracer/ -run TestBatchFlush -v`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/tracer/tracer.go
git commit -m "fix: switch from SimpleSpanProcessor to BatchSpanProcessor

Replaces WithSyncer (synchronous per-span export) with
BatchSpanProcessor (queue in memory, batch flush on shutdown).
This reduces Stop hook latency from 8-12s to ~100-300ms.

Fixes #14"
```

---

### Task 3: Update test helper and all existing tests

**Files:**
- Modify: `internal/tracer/tracer_test.go`

**Step 1: Update `setupTestTracer` to return flush**

```go
func setupTestTracer(t *testing.T) (*tracetest.InMemoryExporter, func(), func()) {
	t.Helper()
	logging.Init(filepath.Join(t.TempDir(), "test.log"), false)
	exporter := tracetest.NewInMemoryExporter()
	shutdown, flush, err := InitTracerWithExporter(exporter)
	if err != nil {
		t.Fatalf("InitTracerWithExporter: %v", err)
	}
	return exporter, shutdown, flush
}
```

**Step 2: Update `TestInitTracerWithExporter`**

```go
func TestInitTracerWithExporter(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	shutdown, _, err := InitTracerWithExporter(exporter)
	if err != nil {
		t.Fatalf("InitTracerWithExporter failed: %v", err)
	}
	defer shutdown()

	spans := exporter.GetSpans()
	if len(spans) != 0 {
		t.Errorf("expected 0 spans initially, got %d", len(spans))
	}
}
```

**Step 3: Update all tests using `setupTestTracer` to add flush before assertions**

Each test follows this pattern — add `flush` to the return, call `flush()` after `ExportSessionTrace` and before `exporter.GetSpans()`:

```go
// TestExportSessionTrace_SingleTurn
exporter, shutdown, flush := setupTestTracer(t)
defer shutdown()
// ... create turns, call ExportSessionTrace ...
flush()
spans := exporter.GetSpans()

// TestExportSessionTrace_WithTools
exporter, shutdown, flush := setupTestTracer(t)
defer shutdown()
// ... create turns, call ExportSessionTrace ...
flush()
spans := exporter.GetSpans()

// TestExportSessionTrace_WithSubagent
exporter, shutdown, flush := setupTestTracer(t)
defer shutdown()
// ... create turns, call ExportSessionTrace ...
flush()
spans := exporter.GetSpans()

// TestExportSessionTrace_IncrementalExport
exporter, shutdown, flush := setupTestTracer(t)
defer shutdown()
// ... first ExportSessionTrace ...
flush()
// ... assertions on first export ...
exporter.Reset()
// ... second ExportSessionTrace ...
flush()
spans := exporter.GetSpans()

// TestExportSessionTrace_SpanAttributes
exporter, shutdown, flush := setupTestTracer(t)
defer shutdown()
// ... create turns, call ExportSessionTrace ...
flush()
spans := exporter.GetSpans()
```

**Step 4: Run full test suite**

Run: `go test ./internal/tracer/ -v`
Expected: All tests PASS

**Step 5: Commit**

```bash
git add internal/tracer/tracer_test.go
git commit -m "test: update all tracer tests for batch flush pattern"
```

---

### Task 4: Run full project test suite

**Step 1: Run all tests**

Run: `go test ./... -v`
Expected: All tests PASS

**Step 2: Build the binary**

Run: `go build ./cmd/cc-trace/`
Expected: Build succeeds with no errors

---

### Task 5: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

**Step 1: Update the Export line in the Architecture section**

Change:
```
- **Export**: `SimpleSpanProcessor` (synchronous) -- required for short-lived CLI
```

To:
```
- **Export**: `BatchSpanProcessor` -- spans queued in memory, flushed on `Shutdown()`
```

**Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for BatchSpanProcessor architecture"
```
