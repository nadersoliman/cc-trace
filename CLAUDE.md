# CLAUDE.md

OTel trace hook for Claude Code. Bridges hook lifecycle events to OpenTelemetry traces exported to the collector via OTLP/HTTP (shares `OTEL_EXPORTER_OTLP_ENDPOINT` with Claude Code's metrics/logs).

Formerly lived in `nadersoliman/k8s-lab` under `hooks/`. Issues #5-#9 on the k8s-lab repo originated from this codebase.

## Build & Install

```bash
make install    # builds and copies binary to ~/.claude/hooks/cc-trace
```

## Architecture

Short-lived CLI invoked by Claude Code on **PostToolUse**, **PostToolUseFailure**, **SubagentStop**, and **Stop** hook events via stdin JSON.

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
| `internal/hook/types.go` | Data structures (HookInput, Turn, ToolCall, SessionState) |
| `internal/logging/logging.go` | Debug and error logging to file |
| `internal/state/state.go` | State file load/save with file locking |
| `internal/transcript/parse.go` | JSONL transcript parsing into turns |
| `internal/tracer/tracer.go` | OTel SDK init, span creation, deterministic trace IDs |

## Environment Variables

All configuration follows the [OTel environment variable specification](https://opentelemetry.io/docs/languages/sdk-configuration/). The Go SDK reads these automatically.

| Variable | OTel Spec Default | Purpose |
|----------|-------------------|---------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4318` | Base OTLP endpoint. The HTTP exporter appends `/v1/traces` automatically. Shared with Claude Code's metrics/logs -- no separate traces endpoint needed. |
| `OTEL_SERVICE_NAME` | `unknown_service` | `service.name` resource attribute |
| `OTEL_RESOURCE_ATTRIBUTES` | (none) | Comma-separated `key=value` pairs added to the trace resource (e.g., `project.name=k8s-lab`) |
| `CC_TRACE_DEBUG` | `false` | Debug logging to `~/.claude/state/cc_trace.log` |
| `CC_TRACE_TIMING` | `false` | Phase-level timing logs to `~/.claude/state/cc_trace.log` (format: `total=Nms EventName session=... phase=Nms ...`) |
| `CC_TRACE_DUMP` | `false` | Dump raw hook payloads and transcripts to `/tmp/cc-trace/dumps/` for investigation |
| `CC_TRACE_ROTATE` | `false` | Rotate trace ID per resume. Each Stop gets its own self-contained trace, preventing long-lived sessions from outliving backend retention. |

**Note:** The hook previously used gRPC (`otlptracegrpc`, port 4317) with a separate `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`. It now uses HTTP/protobuf (`otlptracehttp`) to share the same `OTEL_EXPORTER_OTLP_ENDPOINT` as Claude Code's metrics and logs. Per the OTel spec, the HTTP exporter appends `/v1/traces` to the base endpoint automatically.

## Git Conventions

All commits must use the author flag: `--author="Claude Opus 4.6 <noreply@anthropic.com>"`
