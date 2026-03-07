# Hook Payload Enrichment Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enrich tool spans with PostToolUseFailure data and refactor codebase to typed hook structs + per-hook tracer files.

**Architecture:** Replace the god struct `HookInput` with `HookBase` embedding + per-event structs. Split `tracer.go` into per-hook-event files. Add failure attributes to tool spans.

**Tech Stack:** Go, OpenTelemetry Go SDK, JSON unmarshaling with struct embedding

---

### Task 1: Extract typed hook structs with HookBase embedding

**Files:**
- Modify: `internal/hook/types.go`
- Modify: `cmd/cc-trace/main.go`
- Modify: `cmd/cc-trace/main_test.go`
- Modify: `testdata/fixtures/posttooluse_failure.json`

**Step 1: Replace HookInput with HookBase + per-event structs in types.go**

Replace the `HookInput` struct (lines 6-22) with:

```go
// HookBase contains fields shared across all hook event payloads.
type HookBase struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	PermissionMode string `json:"permission_mode"`
	HookEventName  string `json:"hook_event_name"`
}

// PostToolUsePayload is the schema for PostToolUse hook events.
type PostToolUsePayload struct {
	HookBase
	ToolName     string                 `json:"tool_name"`
	ToolInput    map[string]interface{} `json:"tool_input,omitempty"`
	ToolResponse interface{}            `json:"tool_response,omitempty"`
	ToolUseID    string                 `json:"tool_use_id"`
}

// PostToolUseFailurePayload is the schema for PostToolUseFailure hook events.
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

// SubagentStopPayload is the schema for SubagentStop hook events.
type SubagentStopPayload struct {
	HookBase
	AgentID             string `json:"agent_id"`
	AgentType           string `json:"agent_type"`
	AgentTranscriptPath string `json:"agent_transcript_path"`
	LastAssistantMsg    string `json:"last_assistant_message"`
	StopHookActive      bool   `json:"stop_hook_active"`
}

// StopPayload is the schema for Stop hook events.
type StopPayload struct {
	HookBase
	StopHookActive   bool   `json:"stop_hook_active"`
	LastAssistantMsg string `json:"last_assistant_message"`
}
```

**Step 2: Update main.go to two-phase dispatch**

Replace the current single-unmarshal dispatch (lines 48-76) with:

```go
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		logging.Log("ERROR", fmt.Sprintf("Failed to read stdin: %v", err))
		os.Exit(0)
	}

	// Phase 1: unmarshal base fields to determine event type.
	var base hook.HookBase
	if err := json.Unmarshal(data, &base); err != nil {
		logging.Log("ERROR", fmt.Sprintf("Failed to parse stdin: %v", err))
		os.Exit(0)
	}

	if dumpEnabled {
		dumpPayload(base.HookEventName, base.SessionID, data)
	}

	logging.Debug(fmt.Sprintf("Event: %s, Session: %s", base.HookEventName, base.SessionID))

	// Phase 2: unmarshal into typed struct and dispatch.
	switch base.HookEventName {
	case "PostToolUse":
		var input hook.PostToolUsePayload
		if err := json.Unmarshal(data, &input); err != nil {
			logging.Log("ERROR", fmt.Sprintf("Failed to parse PostToolUse: %v", err))
			os.Exit(0)
		}
		handlePostToolUse(input)
	case "PostToolUseFailure":
		var input hook.PostToolUseFailurePayload
		if err := json.Unmarshal(data, &input); err != nil {
			logging.Log("ERROR", fmt.Sprintf("Failed to parse PostToolUseFailure: %v", err))
			os.Exit(0)
		}
		handlePostToolUseFailure(input)
	case "SubagentStop":
		var input hook.SubagentStopPayload
		if err := json.Unmarshal(data, &input); err != nil {
			logging.Log("ERROR", fmt.Sprintf("Failed to parse SubagentStop: %v", err))
			os.Exit(0)
		}
		handleSubagentStop(input)
	case "Stop":
		var input hook.StopPayload
		if err := json.Unmarshal(data, &input); err != nil {
			logging.Log("ERROR", fmt.Sprintf("Failed to parse Stop: %v", err))
			os.Exit(0)
		}
		handleStop(input)
	default:
		logging.Debug(fmt.Sprintf("Unknown event: %s", base.HookEventName))
	}
```

**Step 3: Update handler signatures in main.go**

Update each handler function to accept its typed payload instead of `hook.HookInput`:

- `handlePostToolUse(input hook.PostToolUsePayload)` -- access `input.SessionID` etc. via promoted fields
- `handlePostToolUseFailure(input hook.PostToolUseFailurePayload)` -- new function, initially same body as `handlePostToolUse` but accepting the failure type
- `handleSubagentStop(input hook.SubagentStopPayload)` -- update field references (`input.AgentTranscriptPath` stays the same via embedding)
- `handleStop(input hook.StopPayload)` -- update field references

The `handlePostToolUseFailure` handler stores tool data the same way as `handlePostToolUse` for now. The failure-specific fields (`Error`, `IsInterrupt`) are stored in Task 4.

**Step 4: Update test fixture**

Update `testdata/fixtures/posttooluse_failure.json` to include `permission_mode`:

```json
{
  "session_id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
  "transcript_path": "/home/testuser/.claude/projects/test-project/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.jsonl",
  "cwd": "/home/testuser/projects/cc-trace",
  "permission_mode": "default",
  "hook_event_name": "PostToolUseFailure",
  "tool_name": "Bash",
  "tool_input": {
    "command": "make build"
  },
  "tool_use_id": "toolu_test_003",
  "error": "Exit code 1\nbuild failed",
  "is_interrupt": false
}
```

**Step 5: Update main_test.go**

Update `loadFixtureInput` and all tests to use the typed structs:

- Replace `hook.HookInput` references with the appropriate typed payload
- Create separate `loadFixture[PayloadType]` helpers or a generic approach
- `TestHandlePostToolUse_Failure` should call `handlePostToolUseFailure` (not `handlePostToolUse`)
- Update `TestFullFlow` step ordering to use typed handlers

**Step 6: Run tests to verify no behavior change**

Run: `go test ./... -v`
Expected: All existing tests pass. Build succeeds.

**Step 7: Commit**

```bash
git add internal/hook/types.go cmd/cc-trace/main.go cmd/cc-trace/main_test.go testdata/fixtures/posttooluse_failure.json
git commit --author="Claude Opus 4.6 <noreply@anthropic.com>" \
  -m "refactor: extract typed hook structs with HookBase embedding"
```

---

### Task 2: Extract Stop span creation into stop.go

**Files:**
- Modify: `internal/tracer/tracer.go` (remove span creation code)
- Create: `internal/tracer/stop.go` (receives span creation code)

**Step 1: Create stop.go with the span creation logic**

Move from `tracer.go` lines 164-252 (the turn/LLM/tool/subagent span creation loop, `matchSubagent`, `emitSubagentSpans`) into `stop.go`. Keep `ExportSessionTrace` in `tracer.go` but have it call into `stop.go` functions.

Extract these functions into `stop.go`:
- `createTurnSpans(tracer trace.Tracer, sessionCtx context.Context, turns []hook.Turn, toolSpanData []hook.ToolSpanData, pendingSubagents []hook.PendingSubagent)` -- the main loop from lines 165-246
- `matchSubagent` (lines 256-269) -- move as-is
- `emitSubagentSpans` (lines 272-345) -- move as-is

`ExportSessionTrace` in `tracer.go` becomes:

```go
func ExportSessionTrace(sessionID string, turns []hook.Turn, toolSpanData []hook.ToolSpanData, pendingSubagents []hook.PendingSubagent, ss *hook.SessionState, rotate bool) {
	// ... existing session span logic (lines 97-162) stays here ...

	// Delegate span creation to stop.go
	createTurnSpans(tracer, sessionCtx, turns, toolSpanData, pendingSubagents)

	// End session span only if it was created in this invocation.
	if sessionSpan != nil {
		sessionSpan.End(trace.WithTimestamp(sessionEnd))
	}
	logging.Debug(fmt.Sprintf("Exported %d turns for session %s", len(turns), truncate(sessionID, 12)))
}
```

**Step 2: Verify imports compile**

Both files are in `package tracer` so they share access. Ensure `stop.go` has the necessary imports (`context`, `encoding/json`, `fmt`, `go.opentelemetry.io/otel/attribute`, `go.opentelemetry.io/otel/codes`, `go.opentelemetry.io/otel/trace`, `cc-trace/internal/hook`, `cc-trace/internal/logging`).

**Step 3: Run tests to verify no behavior change**

Run: `go test ./... -v`
Expected: All existing tests pass unchanged -- same public API, just split across files.

**Step 4: Commit**

```bash
git add internal/tracer/tracer.go internal/tracer/stop.go
git commit --author="Claude Opus 4.6 <noreply@anthropic.com>" \
  -m "refactor: extract Stop span creation into stop.go"
```

---

### Task 3: Extract PostToolUse logic into posttooluse.go

**Files:**
- Modify: `internal/tracer/stop.go` (remove tool span attribute building)
- Create: `internal/tracer/posttooluse.go` (tool span attribute building)

**Step 1: Extract tool span attribute building into posttooluse.go**

Move the tool span attribute building and ToolSpanData enrichment logic from the tool span section of `stop.go` into a function in `posttooluse.go`:

```go
package tracer

import (
	"encoding/json"

	"cc-trace/internal/hook"

	"go.opentelemetry.io/otel/attribute"
)

// buildToolAttrs builds the span attributes for a successful tool call.
func buildToolAttrs(tc hook.ToolCall, toolSpanData []hook.ToolSpanData) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("tool.name", tc.Name),
		attribute.String("tool.use_id", tc.ID),
		attribute.Bool("tool.success", tc.Success),
	}
	if tc.Input != nil {
		if inputJSON, err := json.Marshal(tc.Input); err == nil {
			attrs = append(attrs, attribute.String("tool.input", truncate(string(inputJSON), 4096)))
		}
	}
	// Enrich from PostToolUse data if available.
	for _, tsd := range toolSpanData {
		if tsd.ToolUseID == tc.ID && tsd.ToolResponse != nil {
			if respJSON, err := json.Marshal(tsd.ToolResponse); err == nil {
				attrs = append(attrs, attribute.String("tool.response", truncate(string(respJSON), 4096)))
			}
			break
		}
	}
	return attrs
}
```

Update `stop.go` tool span section to call `buildToolAttrs(tc, toolSpanData)`.

**Step 2: Run tests**

Run: `go test ./... -v`
Expected: All tests pass.

**Step 3: Commit**

```bash
git add internal/tracer/stop.go internal/tracer/posttooluse.go
git commit --author="Claude Opus 4.6 <noreply@anthropic.com>" \
  -m "refactor: extract PostToolUse logic into posttooluse.go"
```

---

### Task 4: Extract PostToolUseFailure into posttoolusefailure.go and extend ToolSpanData

**Files:**
- Modify: `internal/hook/types.go` (extend `ToolSpanData`)
- Modify: `cmd/cc-trace/main.go` (store failure fields in `ToolSpanData`)
- Create: `internal/tracer/posttoolusefailure.go`

**Step 1: Extend ToolSpanData with failure fields**

Add to `ToolSpanData` in `types.go`:

```go
type ToolSpanData struct {
	ToolName       string                 `json:"tool_name"`
	ToolUseID      string                 `json:"tool_use_id"`
	ToolInput      map[string]interface{} `json:"tool_input,omitempty"`
	ToolResponse   interface{}            `json:"tool_response,omitempty"`
	Timestamp      time.Time              `json:"timestamp"`
	Error          string                 `json:"error,omitempty"`
	IsInterrupt    bool                   `json:"is_interrupt,omitempty"`
	HookEventName  string                 `json:"hook_event_name,omitempty"`
	PermissionMode string                 `json:"permission_mode,omitempty"`
	AgentID        string                 `json:"agent_id,omitempty"`
	AgentType      string                 `json:"agent_type,omitempty"`
}
```

**Step 2: Update handlePostToolUseFailure in main.go**

Store the failure-specific fields when recording tool data:

```go
func handlePostToolUseFailure(input hook.PostToolUseFailurePayload) {
	start := time.Now()

	if input.SessionID == "" {
		logging.Debug("No session_id in PostToolUseFailure, skipping")
		return
	}

	lockStart := time.Now()
	if !state.AcquireLock() {
		logging.Debug("Lock held, skipping PostToolUseFailure")
		return
	}
	defer state.ReleaseLock()
	lockDur := time.Since(lockStart)

	loadStart := time.Now()
	sf := state.LoadState()
	loadDur := time.Since(loadStart)

	ss, ok := sf.Sessions[input.SessionID]
	if !ok {
		ss = &hook.SessionState{
			TranscriptPath: input.TranscriptPath,
			CWD:            input.CWD,
		}
		sf.Sessions[input.SessionID] = ss
	}

	ss.ToolSpans = append(ss.ToolSpans, hook.ToolSpanData{
		ToolName:       input.ToolName,
		ToolUseID:      input.ToolUseID,
		ToolInput:      input.ToolInput,
		Timestamp:      time.Now(),
		Error:          input.Error,
		IsInterrupt:    input.IsInterrupt,
		HookEventName:  input.HookEventName,
		PermissionMode: input.PermissionMode,
		AgentID:        input.AgentID,
		AgentType:      input.AgentType,
	})
	ss.Updated = time.Now()

	saveStart := time.Now()
	if err := state.SaveState(sf); err != nil {
		logging.Log("ERROR", fmt.Sprintf("Failed to save state: %v", err))
	}
	saveDur := time.Since(saveStart)

	logging.Debug(fmt.Sprintf("Recorded tool failure: %s (%s) error=%q", input.ToolName, input.ToolUseID, input.Error))

	sid := input.SessionID
	if len(sid) > 8 {
		sid = sid[:8]
	}
	logging.Timing(fmt.Sprintf("total=%dms PostToolUseFailure session=%s tool=%s lock=%dms load=%dms save=%dms",
		time.Since(start).Milliseconds(), sid, input.ToolName,
		lockDur.Milliseconds(), loadDur.Milliseconds(), saveDur.Milliseconds()))
}
```

**Step 3: Create posttoolusefailure.go**

```go
package tracer

import (
	"encoding/json"

	"cc-trace/internal/hook"

	"go.opentelemetry.io/otel/attribute"
)

// buildToolFailureAttrs builds span attributes for a failed tool call,
// enriching with PostToolUseFailure-specific fields from ToolSpanData.
func buildToolFailureAttrs(tc hook.ToolCall, tsd hook.ToolSpanData) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("tool.name", tc.Name),
		attribute.String("tool.use_id", tc.ID),
		attribute.Bool("tool.success", tc.Success),
	}
	if tc.Input != nil {
		if inputJSON, err := json.Marshal(tc.Input); err == nil {
			attrs = append(attrs, attribute.String("tool.input", truncate(string(inputJSON), 4096)))
		}
	}
	if tsd.Error != "" {
		attrs = append(attrs, attribute.String("error", tsd.Error))
	}
	attrs = append(attrs, attribute.Bool("is_interrupt", tsd.IsInterrupt))
	if tsd.HookEventName != "" {
		attrs = append(attrs, attribute.String("hook.event_name", tsd.HookEventName))
	}
	if tsd.PermissionMode != "" {
		attrs = append(attrs, attribute.String("permission_mode", tsd.PermissionMode))
	}
	if tsd.AgentID != "" {
		attrs = append(attrs, attribute.String("agent.id", tsd.AgentID))
	}
	if tsd.AgentType != "" {
		attrs = append(attrs, attribute.String("agent.type", tsd.AgentType))
	}
	return attrs
}
```

**Step 4: Update main_test.go**

Update `TestHandlePostToolUse_Failure` to verify the new fields are stored:

```go
func TestHandlePostToolUseFailure_Integration(t *testing.T) {
	setupTestStateDir(t)

	data, err := os.ReadFile(fixturePath(t, "posttooluse_failure.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var input hook.PostToolUseFailurePayload
	if err := json.Unmarshal(data, &input); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	handlePostToolUseFailure(input)

	sf := state.LoadState()
	ss, ok := sf.Sessions[input.SessionID]
	if !ok {
		t.Fatalf("session %q not found in state", input.SessionID)
	}
	if len(ss.ToolSpans) != 1 {
		t.Fatalf("expected 1 ToolSpan, got %d", len(ss.ToolSpans))
	}
	tsd := ss.ToolSpans[0]
	if tsd.Error != "Exit code 1\nbuild failed" {
		t.Errorf("Error = %q, want %q", tsd.Error, "Exit code 1\nbuild failed")
	}
	if tsd.IsInterrupt != false {
		t.Errorf("IsInterrupt = %v, want false", tsd.IsInterrupt)
	}
	if tsd.HookEventName != "PostToolUseFailure" {
		t.Errorf("HookEventName = %q, want %q", tsd.HookEventName, "PostToolUseFailure")
	}
}
```

**Step 5: Run tests**

Run: `go test ./... -v`
Expected: All tests pass. New fields zero-valued in existing test data -- no breakage.

**Step 6: Commit**

```bash
git add internal/hook/types.go cmd/cc-trace/main.go cmd/cc-trace/main_test.go internal/tracer/posttoolusefailure.go
git commit --author="Claude Opus 4.6 <noreply@anthropic.com>" \
  -m "refactor: extract PostToolUseFailure into posttoolusefailure.go and extend ToolSpanData"
```

---

### Task 5: Extract SubagentStop span creation into subagentstop.go

**Files:**
- Modify: `internal/tracer/stop.go` (remove `emitSubagentSpans`, `matchSubagent`)
- Create: `internal/tracer/subagentstop.go` (receives subagent span logic)

**Step 1: Move matchSubagent and emitSubagentSpans to subagentstop.go**

Move both functions from `stop.go` to `subagentstop.go` as-is. No signature changes.

```go
package tracer

import (
	"context"
	"encoding/json"
	"fmt"

	"cc-trace/internal/hook"
	"cc-trace/internal/logging"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// matchSubagent finds a pending subagent whose execution window overlaps the Task tool call.
func matchSubagent(pending []hook.PendingSubagent, matched []bool, tc hook.ToolCall) (*hook.PendingSubagent, int) {
	// ... existing code from tracer.go lines 256-269 ...
}

// emitSubagentSpans creates child spans for a subagent under the Task tool span.
func emitSubagentSpans(tracer trace.Tracer, taskSpan trace.Span, turnCtx context.Context, sub hook.PendingSubagent) {
	// ... existing code from tracer.go lines 272-345 ...
}
```

**Step 2: Run tests**

Run: `go test ./... -v`
Expected: All tests pass.

**Step 3: Commit**

```bash
git add internal/tracer/stop.go internal/tracer/subagentstop.go
git commit --author="Claude Opus 4.6 <noreply@anthropic.com>" \
  -m "refactor: extract SubagentStop span creation into subagentstop.go"
```

---

### Task 6: Add PostToolUseFailure attributes to tool spans

**Files:**
- Modify: `internal/tracer/stop.go` (use `buildToolFailureAttrs` when ToolSpanData has Error)
- Modify: `internal/tracer/tracer_test.go` (new test)
- Modify: `internal/tracer/CLAUDE.md` (update attribute map)

**Step 1: Write the failing test**

Add to `tracer_test.go`:

```go
func TestExportSessionTrace_ToolFailureAttributes(t *testing.T) {
	exporter, shutdown, flush := setupTestTracer(t)
	defer shutdown()

	sessionID := "test-session-tool-failure"
	now := time.Now()

	turns := []hook.Turn{
		{
			Number:    1,
			Model:     "claude-sonnet-4-20250514",
			StartTime: now,
			EndTime:   now.Add(3 * time.Second),
			ToolCalls: []hook.ToolCall{
				{
					Name:      "WebFetch",
					ID:        "toolu_fail_001",
					Success:   false,
					StartTime: now.Add(500 * time.Millisecond),
					EndTime:   now.Add(1 * time.Second),
				},
			},
		},
	}

	toolSpanData := []hook.ToolSpanData{
		{
			ToolName:       "WebFetch",
			ToolUseID:      "toolu_fail_001",
			Error:          "unable to verify the first certificate",
			IsInterrupt:    false,
			HookEventName:  "PostToolUseFailure",
			PermissionMode: "acceptEdits",
			AgentID:        "agent-sub-001",
			AgentType:      "general-purpose",
		},
	}

	ss := &hook.SessionState{}
	ExportSessionTrace(sessionID, turns, toolSpanData, nil, ss, false)

	flush()
	spans := exporter.GetSpans()

	toolSpan := findSpan(spans, "Tool: WebFetch")
	if toolSpan == nil {
		t.Fatal("expected 'Tool: WebFetch' span")
	}

	// Verify failure attributes
	if v := getAttr(toolSpan, "error"); v != "unable to verify the first certificate" {
		t.Errorf("error = %v, want 'unable to verify the first certificate'", v)
	}
	if v := getAttr(toolSpan, "is_interrupt"); v != false {
		t.Errorf("is_interrupt = %v, want false", v)
	}
	if v := getAttr(toolSpan, "hook.event_name"); v != "PostToolUseFailure" {
		t.Errorf("hook.event_name = %v, want PostToolUseFailure", v)
	}
	if v := getAttr(toolSpan, "permission_mode"); v != "acceptEdits" {
		t.Errorf("permission_mode = %v, want acceptEdits", v)
	}
	if v := getAttr(toolSpan, "agent.id"); v != "agent-sub-001" {
		t.Errorf("agent.id = %v, want agent-sub-001", v)
	}
	if v := getAttr(toolSpan, "agent.type"); v != "general-purpose" {
		t.Errorf("agent.type = %v, want general-purpose", v)
	}
	if v := getAttr(toolSpan, "tool.success"); v != false {
		t.Errorf("tool.success = %v, want false", v)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tracer/ -run TestExportSessionTrace_ToolFailureAttributes -v`
Expected: FAIL -- `error` attribute not found on tool span.

**Step 3: Wire failure attributes into stop.go tool span creation**

In the tool span section of `stop.go`, update the ToolSpanData matching to detect failures and use `buildToolFailureAttrs`:

```go
		// Tool spans.
		for _, tc := range turn.ToolCalls {
			var attrs []attribute.KeyValue

			// Check if we have failure data for this tool call.
			var failureTSD *hook.ToolSpanData
			for i, tsd := range toolSpanData {
				if tsd.ToolUseID == tc.ID && tsd.Error != "" {
					failureTSD = &toolSpanData[i]
					break
				}
			}

			if failureTSD != nil {
				attrs = buildToolFailureAttrs(tc, *failureTSD)
			} else {
				attrs = buildToolAttrs(tc, toolSpanData)
			}

			// ... rest of tool span creation unchanged ...
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tracer/ -run TestExportSessionTrace_ToolFailureAttributes -v`
Expected: PASS

**Step 5: Run all tests**

Run: `go test ./... -v`
Expected: All tests pass.

**Step 6: Update internal/tracer/CLAUDE.md**

Add the new attributes to the span attribute map table:

```
| Tool | `error` | string | PostToolUseFailure payload `error` |
| Tool | `is_interrupt` | bool | PostToolUseFailure payload `is_interrupt` |
| Tool | `hook.event_name` | string | Hook payload `hook_event_name` |
| Tool | `permission_mode` | string | Hook payload `permission_mode` |
| Tool | `agent.id` | string | PostToolUseFailure payload `agent_id` (subagent context) |
| Tool | `agent.type` | string | PostToolUseFailure payload `agent_type` (subagent context) |
```

**Step 7: Commit**

```bash
git add internal/tracer/stop.go internal/tracer/tracer_test.go internal/tracer/CLAUDE.md
git commit --author="Claude Opus 4.6 <noreply@anthropic.com>" \
  -m "feat: add PostToolUseFailure attributes to tool spans"
```

---

## Verification

After all 6 commits:

```bash
go test ./... -v          # All tests pass
go build ./...            # Clean build
go vet ./...              # No warnings
make install              # Binary deployed
```

Verify end-to-end by triggering a PostToolUseFailure (e.g., WebFetch to a bad URL) and checking the tool span in Grafana Tempo for `error`, `is_interrupt`, `hook.event_name`, `permission_mode` attributes.
