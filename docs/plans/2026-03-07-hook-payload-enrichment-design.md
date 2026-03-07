# Hook Payload Enrichment & Per-Hook Tracer Refactoring

## Problem

1. The `HookInput` struct is a god struct mashing all four hook schemas together with `omitempty`. Fields like `error` and `is_interrupt` from PostToolUseFailure are silently dropped -- never added because the struct wasn't designed around the actual payloads.

2. `tracer.go` is a monolith handling init, trace ID generation, session spans, and all span creation for every hook event type.

3. Tool failure spans only have `tool.success=false` with no indication of why the failure occurred.

## Solution

Three pillars:

1. **Typed hook structs** -- one struct per hook schema with `HookBase` embedding (Go idiom for shared fields)
2. **Per-hook tracer files** -- split tracer.go following the hook event model
3. **PostToolUseFailure span enrichment** -- new span attributes from the failure payload

## Typed Hook Structs

Replace `HookInput` with `HookBase` + per-event structs using Go struct embedding:

```go
type HookBase struct {
    SessionID      string `json:"session_id"`
    TranscriptPath string `json:"transcript_path"`
    CWD            string `json:"cwd"`
    PermissionMode string `json:"permission_mode"`
    HookEventName  string `json:"hook_event_name"`
}

type PostToolUsePayload struct {
    HookBase
    ToolName     string                 `json:"tool_name"`
    ToolInput    map[string]interface{} `json:"tool_input,omitempty"`
    ToolResponse interface{}            `json:"tool_response,omitempty"`
    ToolUseID    string                 `json:"tool_use_id"`
}

type PostToolUseFailurePayload struct {
    HookBase
    ToolName    string                 `json:"tool_name"`
    ToolInput   map[string]interface{} `json:"tool_input,omitempty"`
    ToolUseID   string                 `json:"tool_use_id"`
    Error       string                 `json:"error"`
    IsInterrupt bool                   `json:"is_interrupt"`
    AgentID     string                 `json:"agent_id,omitempty"`
    AgentType   string                 `json:"agent_type,omitempty"`
}

type SubagentStopPayload struct {
    HookBase
    AgentID             string `json:"agent_id"`
    AgentType           string `json:"agent_type"`
    AgentTranscriptPath string `json:"agent_transcript_path"`
    LastAssistantMsg    string `json:"last_assistant_message"`
    StopHookActive      bool   `json:"stop_hook_active"`
}

type StopPayload struct {
    HookBase
    StopHookActive   bool   `json:"stop_hook_active"`
    LastAssistantMsg string `json:"last_assistant_message"`
}
```

Dispatch in `main.go` uses two-phase unmarshal: `HookBase` first to read `hook_event_name`, then full bytes into the typed struct.

`ToolSpanData` extended with: `Error`, `IsInterrupt`, `HookEventName`, `PermissionMode`, `AgentID`, `AgentType`.

## Per-Hook Tracer Files

Split following the hook event model (option B from design discussion):

| File | Hook Event | Responsibility |
|------|-----------|---------------|
| `tracer.go` | (shared) | Init, shutdown, trace ID generation, session span logic |
| `stop.go` | Stop | `ExportSessionTrace` -- orchestrates turn/LLM/tool/subagent span creation from transcript |
| `posttooluse.go` | PostToolUse | Maps successful tool payload fields to span attributes |
| `posttoolusefailure.go` | PostToolUseFailure | Maps failure payload fields to span attributes |
| `subagentstop.go` | SubagentStop | Subagent span creation from parsed subagent transcript |

## New Span Attributes (PostToolUseFailure)

Following the naming convention from `internal/tracer/CLAUDE.md`:

| Hook field | Span attribute | Type | Naming rule | Notes |
|---|---|---|---|---|
| `error` | `error` | string | Flat field | Failure reason |
| `is_interrupt` | `is_interrupt` | bool | Flat field | Whether user interrupted |
| `tool_input` | `tool.input` | string | Domain-prefixed | JSON-stringified, truncated to 500 chars |
| `permission_mode` | `permission_mode` | string | Flat field | Session permission level |
| `hook_event_name` | `hook.event_name` | string | Domain-prefixed | PostToolUse vs PostToolUseFailure |
| `agent_id` | `agent.id` | string | Domain-prefixed | Present when failure is inside a subagent |
| `agent_type` | `agent.type` | string | Domain-prefixed | Present when failure is inside a subagent |

`tool.input` is JSON-stringified (not decomposed into `tool.input.url`, `tool.input.prompt`) because each tool has a different input schema. A generic string keeps it tool-agnostic and queryable via TraceQL `=~` regex.

## Commit Sequence

Each commit is a complete, testable change. Tests pass after every commit.

| # | Commit | What changes |
|---|--------|-------------|
| 1 | refactor: extract typed hook structs with HookBase embedding | `types.go`: Replace `HookInput` with `HookBase` + per-event structs. `main.go`: two-phase unmarshal. No behavior change. |
| 2 | refactor: extract Stop span creation into stop.go | Move turn/LLM/tool/subagent span creation from `tracer.go` into `stop.go`. |
| 3 | refactor: extract PostToolUse logic into posttooluse.go | Move tool span data preparation into `posttooluse.go`. |
| 4 | refactor: extract PostToolUseFailure into posttoolusefailure.go | New file for failure path. `ToolSpanData` gains new fields. |
| 5 | refactor: extract SubagentStop span creation into subagentstop.go | Move subagent span creation into `subagentstop.go`. |
| 6 | feat: add PostToolUseFailure attributes to tool spans | New attributes on tool spans. New tests for failure attributes. |

Commits 1-5: pure refactoring, no behavior change, existing tests are the safety net.
Commit 6: the feature, with new tests.

## Files Changed

| File | Change |
|------|--------|
| `internal/hook/types.go` | `HookInput` replaced with `HookBase` + 4 typed structs, `ToolSpanData` extended |
| `internal/tracer/tracer.go` | Slimmed to init, trace ID, session span |
| `internal/tracer/stop.go` | New -- turn/LLM span creation |
| `internal/tracer/posttooluse.go` | New -- tool span attribute mapping |
| `internal/tracer/posttoolusefailure.go` | New -- failure span attribute mapping |
| `internal/tracer/subagentstop.go` | New -- subagent span creation |
| `internal/tracer/CLAUDE.md` | Updated attribute map |
| `cmd/cc-trace/main.go` | Two-phase dispatch, typed handlers |
