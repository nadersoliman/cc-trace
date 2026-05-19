# CLAUDE.md

OTel trace hook for Claude Code. Bridges hook lifecycle events to OpenTelemetry traces exported to the collector via OTLP/HTTP (shares `OTEL_EXPORTER_OTLP_ENDPOINT` with Claude Code's metrics/logs).

Formerly lived in `nadersoliman/k8s-lab` under `hooks/`. Issues #5-#9 on the k8s-lab repo originated from this codebase.

## Build & Install

```bash
make install    # builds and copies binary to ~/.claude/hooks/cc-trace
```

## Architecture

Short-lived CLI invoked by Claude Code on **SessionStart**, **PostToolUse**, **PostToolUseFailure**, **SubagentStop**, and **Stop** hook events via stdin JSON.

- **SessionStart** (< 10ms, no network): Rotates trace ID when `CC_TRACE_ROTATE=true` (increments epoch, clears SessionSpanID)
- **PostToolUse / PostToolUseFailure** (< 10ms, no network): Records tool data to `~/.claude/state/cc_trace_state.json`
- **SubagentStop** (< 50ms, no network): Parses subagent transcript and stores for later export
- **Stop** (< 2s): Parses JSONL transcript, creates OTel spans, exports via OTLP/HTTP, updates state

## Span Hierarchy

```
Session Root
  └── Turn N
       ├── LLM Response (model, tokens)
       ├── Tool: Read
       ├── Tool: Task
       │    └── Subagent: general-purpose
       │         └── Turn 1
       │              ├── LLM Response
       │              └── Tool: Grep
       └── Tool: Edit
```

- **Trace ID**: Deterministic `SHA-256(session_id)[:16]` -- consistent across invocations
- **TRACEPARENT**: Honors W3C `TRACEPARENT` env var for external trace correlation
- **Timing**: From transcript timestamps (real wall-clock, not hook execution time)
- **Export**: `BatchSpanProcessor` -- spans queued in memory, flushed on `Shutdown()`

## Files

| Path | Purpose |
|------|---------|
| `cmd/cc-trace/main.go` | Entry point, stdin parsing, event dispatch |
| `internal/hook/types.go` | Data structures (HookBase, typed payloads, Turn, ToolCall, SessionState) |
| `internal/logging/logging.go` | Debug and error logging to file |
| `internal/state/state.go` | State file load/save with file locking |
| `internal/transcript/parse.go` | JSONL transcript parsing into turns |
| `internal/tracer/tracer.go` | OTel SDK init, session span orchestration, deterministic trace IDs |
| `internal/tracer/stop.go` | Turn/LLM/tool span creation from transcript |
| `internal/tracer/posttooluse.go` | Tool span attribute building for successful calls |
| `internal/tracer/posttoolusefailure.go` | Tool span attribute building for failed calls |
| `internal/tracer/subagentstop.go` | Subagent span creation from parsed subagent transcript |

## Environment Variables

### OTel Config Relay (`CC_TRACE_OTEL_*`)

Claude Code v2.1.128+ strips `OTEL_*` env vars from hook subprocesses. cc-trace works around this by reading `CC_TRACE_OTEL_*` vars at startup, stripping the `CC_TRACE_` prefix, and setting the corresponding `OTEL_*` var via `os.Setenv` — but only if the target is not already present (direct `OTEL_*` wins). This happens in `relayOtelEnv()` in `main.go`, before `InitTracer`.

| Variable | OTel Spec Default | Purpose |
|----------|-------------------|---------|
| `CC_TRACE_OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4318` | Base OTLP endpoint. HTTP exporter appends `/v1/traces` automatically. |
| `CC_TRACE_OTEL_SERVICE_NAME` | `unknown_service` | `service.name` resource attribute |
| `CC_TRACE_OTEL_RESOURCE_ATTRIBUTES` | (none) | Comma-separated `key=value` pairs added to the trace resource (e.g., `project.name=k8s-lab`) |
| `CC_TRACE_OTEL_EXPORTER_OTLP_HEADERS` | (none) | Auth headers for OTLP requests (e.g., `Authorization=Bearer xxx`) |

Any `CC_TRACE_OTEL_*` var is relayed — the mechanism is a mechanical prefix strip, not a whitelist.

### cc-trace Settings

| Variable | Default | Purpose |
|----------|---------|---------|
| `CC_TRACE_DEBUG` | `false` | Debug logging to `~/.claude/state/cc_trace.log` |
| `CC_TRACE_TIMING` | `false` | Phase-level timing logs to `~/.claude/state/cc_trace.log` (format: `total=Nms EventName session=... phase=Nms ...`) |
| `CC_TRACE_DUMP` | `false` | Dump raw hook payloads and transcripts to `/tmp/cc-trace/dumps/` for investigation |
| `CC_TRACE_ROTATE` | `false` | Rotate trace ID per session segment. Each SessionStart (startup/resume/clear/compact) on an existing session creates a new trace, preventing long-lived sessions from outliving backend retention. Ignored when `TRACEPARENT` is set. |

**Note:** The hook previously used gRPC (`otlptracegrpc`, port 4317) with a separate `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`. It now uses HTTP/protobuf (`otlptracehttp`) to share the same `OTEL_EXPORTER_OTLP_ENDPOINT` as Claude Code's metrics and logs. Per the OTel spec, the HTTP exporter appends `/v1/traces` to the base endpoint automatically. TLS is enabled automatically for `https://` endpoints; set `OTEL_EXPORTER_OTLP_HEADERS` for auth headers.

## Documentation

See [`docs/CLAUDE.md`](docs/CLAUDE.md) for documentation structure, folder layout, and templates (plans, spikes).

## Git Conventions

All commits must use the author flag: `--author="Claude Opus 4.6 <noreply@anthropic.com>"`
