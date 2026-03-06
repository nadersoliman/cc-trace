# Per-Resume Trace Rotation

## Problem

A single deterministic trace ID per session (`SHA256(session_id)[:16]`) means long-lived sessions produce traces that outlive backend retention (7 days in Tempo). Session `f5df38cb` accumulated 1087 turns over 3 weeks -- early spans aged out, making the trace incomplete.

## Solution

Rotate the trace ID each time a session resumes. One trace = one conversation (from first PostToolUse to Stop). Gated behind `CC_TRACE_ROTATE` feature flag (default: off).

## Trace ID Generation

```
Current:  SHA256(session_id)[:16]
Rotated:  SHA256(session_id + ":" + epoch)[:16]
```

When `CC_TRACE_ROTATE=true`, `epoch` is an integer stored in `SessionState`, incremented after each Stop. Epoch 0 produces the same trace ID as the current behavior (no suffix added for backward compatibility).

## Resume Detection

On each Stop (when `CC_TRACE_ROTATE=true`):

1. Export spans using the current epoch's trace ID
2. Increment the epoch
3. Clear `SessionSpanID` (forces next Stop to create a new Session root span)

Every Stop produces a self-contained trace: Session root + Turn(s) + Tool spans.

## State Changes

Add `Epoch int` to `SessionState`. Zero-value means epoch 0 -- backward compatible, no migration needed.

## Span Hierarchy Per Trace

```
Session: <session_id> epoch N
  +-- Turn 36
  |    +-- LLM Response
  |    +-- Tool: Read
  |    +-- Tool: Edit
  +-- Turn 37
       +-- LLM Response
       +-- Tool: Bash
```

## Session Correlation

All traces from the same session share the `session.id` span attribute (already set on the Session root span). Search Grafana Tempo by `session.id = "f5df38cb-..."` to find all conversation segments.

## Feature Flag

| Variable | Default | Purpose |
|----------|---------|---------|
| `CC_TRACE_ROTATE` | `false` | Rotate trace ID per resume. Each Stop gets its own trace. |

## Files Changed

| File | Change |
|------|--------|
| `internal/hook/types.go` | Add `Epoch int` to `SessionState` |
| `internal/tracer/tracer.go` | `traceIDFromSession` accepts epoch; `ExportSessionTrace` always creates Session root when rotating |
| `cmd/cc-trace/main.go` | Read `CC_TRACE_ROTATE`, pass to export, increment epoch + clear span ID after export |
| `internal/tracer/tracer_test.go` | Tests for epoch-based trace ID generation and rotation |
| `CLAUDE.md` | Add `CC_TRACE_ROTATE` to env var table |
| `README.md` | Add `CC_TRACE_ROTATE` to env var table |
