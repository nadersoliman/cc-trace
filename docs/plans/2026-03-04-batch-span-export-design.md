# Batch Span Export Design

Fixes [issue #14](https://github.com/nadersoliman/cc-trace/issues/14): Stop hook export phase takes 8-12s per turn.

## Problem

The Stop hook's export phase averages 9.6s (range: 7.5-12.3s), blocking Claude Code's hook pipeline after every assistant turn. All other phases complete in <10ms combined.

Root cause: `SimpleSpanProcessor` (`WithSyncer`) exports each span synchronously via OTLP HTTP. Each `span.End()` call blocks for a full HTTP round-trip (~2-5s with timeout). A typical turn creates 3-5+ spans, so latency compounds to 8-12s.

## Solution

Replace `SimpleSpanProcessor` with `BatchSpanProcessor`. Spans queue in memory on `End()` (~0ms). The existing `tp.Shutdown()` call flushes all queued spans in a single batched OTLP HTTP request.

## Architecture Change

```
Before:  sdktrace.WithSyncer(exporter)  →  SimpleSpanProcessor (sync per-span)
After:   NewBatchSpanProcessor(exporter) →  BatchSpanProcessor (queue + batch flush)
```

### Flow

1. `ExportSessionTrace` creates spans — each `span.End()` queues to in-memory buffer (~0ms)
2. `tp.Shutdown()` flushes all buffered spans in one batched OTLP HTTP request (~100-300ms)
3. Process exits

### Configuration

Default `BatchSpanProcessor` settings are fine for a short-lived CLI:
- `MaxExportBatchSize`: 512 (a turn has ~5-20 spans)
- `BatchTimeout`: 5s (irrelevant — `Shutdown()` flushes immediately)
- `MaxQueueSize`: 2048 (more than enough)

No custom tuning needed.

### Test Impact

With `BatchSpanProcessor`, `span.End()` queues instead of exporting. Tests must call `ForceFlush()` or `Shutdown()` before asserting spans. A `ForceFlush` helper will be added to the tracer package for test use.

## Files Changed

| File | Change |
|------|--------|
| `internal/tracer/tracer.go` | Replace `WithSyncer` with `NewBatchSpanProcessor` |
| `internal/tracer/tracer_test.go` | Add flush before span assertions |
| `CLAUDE.md` | Update export architecture note |

## Target

Stop hook total execution <500ms (down from 8-12s).
