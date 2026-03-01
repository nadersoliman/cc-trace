# Testing Design for cc-trace

## Overview

Add fixture-based testing to cc-trace using real session data from `/tmp/cc-trace/dumps/`, cleaned of PII, to feed each registered hook handler. Coverage spans all three layers: transcript parsing, state management, and OTel span export.

## Approach

Fixture-per-file tests (Approach A): one `_test.go` per source file, fixtures in `testdata/fixtures/`.

## Fixture Preparation

### PII Sanitization

A one-time Go sanitizer script reads raw dumps and applies deterministic replacements:

| Pattern | Replacement |
|---------|-------------|
| `/Users/nadersoliman` | `/home/testuser` |
| `/home/nadersoliman` | `/home/testuser` |
| `nadersoliman` (remaining) | `testuser` |
| Session UUIDs | `aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee` |
| `tool_use_id` values | `toolu_test_<seq>` |
| Agent IDs | `agent_test_<seq>` |
| Absolute project paths | `/home/testuser/projects/cc-trace` |

The sanitizer runs manually, not in CI. It produces committed fixtures.

### Curated Fixtures (~10 files)

| Fixture | Source | Purpose |
|---------|--------|---------|
| `posttooluse_taskupdate.json` | Simple PostToolUse | State recording |
| `posttooluse_read.json` | PostToolUse with file path | PII in tool_input |
| `posttooluse_failure.json` | PostToolUseFailure | Error path |
| `stop_simple.json` | Stop with basic transcript | Full Stop flow |
| `stop_multiturn.json` | Stop + multi-turn transcript | Turn numbering |
| `subagent_stop.json` | SubagentStop | Subagent recording |
| `transcript_simple.jsonl` | 1-2 turns, no tools | Basic parsing |
| `transcript_tools.jsonl` | Turns with tool_use + tool_result | Tool call matching |
| `transcript_subagent.jsonl` | Subagent transcript | Nested parsing |
| `state_with_tools.json` | Pre-populated state | State enrichment |

## Test Files

### `transcript_test.go` -- Parsing Layer

- `TestParseTranscript_Simple` -- Single-turn: turn count, user text, timestamps, model
- `TestParseTranscript_MultiTurn` -- Turn numbering, per-turn assistant messages
- `TestParseTranscript_ToolCalls` -- tool_use/tool_result matching by ID, success/failure
- `TestParseTranscript_StartLine` -- Incremental parsing from startLine > 0
- `TestParseTranscript_StopReason` -- stop_reason extraction
- `TestParseTranscript_DurationMs` -- durationMs from system turn_duration
- `TestMergeAssistantParts` -- Streamed assistant message merging
- `TestExtractUsage` -- Token count parsing
- `TestHelpers` -- msgRole, getTextContent, isToolResult, extractTimestamp

### `state_test.go` -- State Layer

Uses `t.TempDir()` for isolation:

- `TestLoadState_Empty` -- No file -> empty state
- `TestLoadState_Corrupt` -- Invalid JSON -> empty state
- `TestSaveAndLoad` -- Round-trip save/load
- `TestSaveState_PrunesStale` -- 24h-old sessions pruned
- `TestLocking` -- acquireLock/releaseLock
- `TestStaleLockRemoval` -- 5-min-old lock cleanup

### `tracer_test.go` -- Span Export Layer

Uses `tracetest.InMemoryExporter`:

- `TestExportSessionTrace_SingleTurn` -- Session + Turn + LLM Response spans
- `TestExportSessionTrace_WithTools` -- Tool spans under Turn
- `TestExportSessionTrace_WithSubagent` -- Subagent spans under Task tool
- `TestExportSessionTrace_SpanAttributes` -- Attribute verification
- `TestExportSessionTrace_IncrementalExport` -- Session span ID reuse
- `TestTraceIDDeterministic` -- Deterministic trace ID from session ID
- `TestTraceparentParsing` -- TRACEPARENT env var handling

### `main_test.go` -- Integration Layer

- `TestHandlePostToolUse_Integration` -- Fixture -> state file updated
- `TestHandleStop_Integration` -- Fixture + transcript -> spans exported
- `TestHandleSubagentStop_Integration` -- Fixture -> pending subagent stored
- `TestFullFlow` -- PostToolUse -> SubagentStop -> Stop, verify span hierarchy

## Refactoring for Testability

### Minimal changes (1 production code change):

1. **Add `initTracerWithExporter(exporter)`** -- accepts any `sdktrace.SpanExporter`; tests pass `InMemoryExporter`, production passes OTLP HTTP exporter. `initTracer()` delegates to it.

2. **State paths** -- `initStatePaths(homeDir)` already callable; tests call it with `t.TempDir()`.

3. **Handlers** -- already take `HookInput` directly, no stdin mocking needed.

4. **Transcript paths** -- tests point at fixture JSONL files in `testdata/`.

## OTel Verification Strategy

Use `tracetest.NewInMemoryExporter()` from `go.opentelemetry.io/otel/sdk/trace/tracetest`. After calling `exportSessionTrace()`, read exported spans and assert:

- Span names (Session, Turn N, LLM Response, Tool: X, Subagent: Y)
- Parent-child relationships via SpanID/ParentSpanID
- Attributes (session.id, turn.number, gen_ai.*, tool.*, agent.*)
- Status codes (error on failed tools)
- Timestamps match fixture data
