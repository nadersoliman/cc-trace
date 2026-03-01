# Testing Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add fixture-based tests covering all three layers of cc-trace (transcript parsing, state management, OTel span export) using PII-cleaned real session data.

**Architecture:** One `_test.go` file per source file. Fixtures in `testdata/fixtures/` are hand-crafted from real dump data with PII replaced. OTel span verification uses `tracetest.InMemoryExporter`. State tests use `t.TempDir()` for isolation.

**Tech Stack:** Go standard `testing`, `go.opentelemetry.io/otel/sdk/trace/tracetest`

---

### Task 1: Create Fixture Directory and Transcript Fixtures

**Files:**
- Create: `testdata/fixtures/transcript_simple.jsonl`
- Create: `testdata/fixtures/transcript_tools.jsonl`

**Step 1: Create testdata/fixtures directory**

Run: `mkdir -p testdata/fixtures`

**Step 2: Create `transcript_simple.jsonl`**

A minimal 2-turn transcript with no tool calls. Each line is a JSON object. The transcript follows the Claude Code JSONL format: user messages have `type: "user"` with `message: {role: "user", content: [...]}`, assistant messages have `type: "assistant"` with `message: {role: "assistant", model: "...", content: [...], usage: {...}, stop_reason: "..."}`.

Create `testdata/fixtures/transcript_simple.jsonl`:

```jsonl
{"type":"user","sessionId":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","cwd":"/home/testuser/projects/cc-trace","message":{"role":"user","content":[{"type":"text","text":"Hello, what is this project?"}]},"timestamp":"2026-01-15T10:00:00.000Z","uuid":"u1"}
{"type":"assistant","sessionId":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","message":{"role":"assistant","model":"claude-sonnet-4-20250514","id":"msg_test_001","type":"message","content":[{"type":"text","text":"This is an OpenTelemetry trace hook for Claude Code."}],"stop_reason":"end_turn","usage":{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":50,"cache_creation_input_tokens":10}},"timestamp":"2026-01-15T10:00:05.000Z","uuid":"a1"}
{"type":"user","sessionId":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","cwd":"/home/testuser/projects/cc-trace","message":{"role":"user","content":[{"type":"text","text":"How does it work?"}]},"timestamp":"2026-01-15T10:00:10.000Z","uuid":"u2"}
{"type":"assistant","sessionId":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","message":{"role":"assistant","model":"claude-sonnet-4-20250514","id":"msg_test_002","type":"message","content":[{"type":"text","text":"It captures hook events and exports them as OTel spans."}],"stop_reason":"end_turn","usage":{"input_tokens":150,"output_tokens":25,"cache_read_input_tokens":80,"cache_creation_input_tokens":0}},"timestamp":"2026-01-15T10:00:18.000Z","uuid":"a2"}
```

**Step 3: Create `transcript_tools.jsonl`**

A 1-turn transcript where the assistant makes two tool calls (Read and Bash), with corresponding tool_result user messages. Includes a `system` message with `subtype: "turn_duration"`.

Create `testdata/fixtures/transcript_tools.jsonl`:

```jsonl
{"type":"user","sessionId":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","cwd":"/home/testuser/projects/cc-trace","message":{"role":"user","content":[{"type":"text","text":"Read the Makefile and show me the build command"}]},"timestamp":"2026-01-15T10:00:00.000Z","uuid":"u1"}
{"type":"assistant","sessionId":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","message":{"role":"assistant","model":"claude-sonnet-4-20250514","id":"msg_test_010","type":"message","content":[{"type":"text","text":"Let me read the Makefile."},{"type":"tool_use","id":"toolu_test_001","name":"Read","input":{"file_path":"/home/testuser/projects/cc-trace/Makefile"}}],"stop_reason":"tool_use","usage":{"input_tokens":200,"output_tokens":40,"cache_read_input_tokens":100,"cache_creation_input_tokens":0}},"timestamp":"2026-01-15T10:00:03.000Z","uuid":"a1"}
{"type":"user","sessionId":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_test_001","content":"BINARY_NAME = otel_trace_hook\nINSTALL_DIR = $(HOME)/.claude/hooks\n\nbuild:\n\tgo build -o $(BINARY_NAME) ."}]},"timestamp":"2026-01-15T10:00:04.000Z","uuid":"tr1"}
{"type":"assistant","sessionId":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","message":{"role":"assistant","model":"claude-sonnet-4-20250514","id":"msg_test_011","type":"message","content":[{"type":"text","text":"Now let me run it."},{"type":"tool_use","id":"toolu_test_002","name":"Bash","input":{"command":"make build","description":"Build the binary"}}],"stop_reason":"tool_use","usage":{"input_tokens":300,"output_tokens":30,"cache_read_input_tokens":150,"cache_creation_input_tokens":0}},"timestamp":"2026-01-15T10:00:06.000Z","uuid":"a2"}
{"type":"user","sessionId":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_test_002","content":"go build -o otel_trace_hook ."}]},"timestamp":"2026-01-15T10:00:08.000Z","uuid":"tr2"}
{"type":"assistant","sessionId":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","message":{"role":"assistant","model":"claude-sonnet-4-20250514","id":"msg_test_012","type":"message","content":[{"type":"text","text":"The build command is `go build -o otel_trace_hook .`. It compiles the project into a single binary."}],"stop_reason":"end_turn","usage":{"input_tokens":400,"output_tokens":35,"cache_read_input_tokens":200,"cache_creation_input_tokens":0}},"timestamp":"2026-01-15T10:00:12.000Z","uuid":"a3"}
{"type":"system","sessionId":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","subtype":"turn_duration","durationMs":12000,"timestamp":"2026-01-15T10:00:12.500Z","uuid":"sys1"}
```

**Step 4: Commit fixture files**

```bash
git add testdata/fixtures/transcript_simple.jsonl testdata/fixtures/transcript_tools.jsonl
git commit -m "Add transcript fixture files for testing" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 2: Create Hook Input Fixtures

**Files:**
- Create: `testdata/fixtures/posttooluse_taskupdate.json`
- Create: `testdata/fixtures/posttooluse_read.json`
- Create: `testdata/fixtures/posttooluse_failure.json`
- Create: `testdata/fixtures/stop_simple.json`
- Create: `testdata/fixtures/subagent_stop.json`

**Step 1: Create `posttooluse_taskupdate.json`**

Based on the real PostToolUse dump for TaskUpdate, with PII replaced:

```json
{
  "session_id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
  "transcript_path": "/home/testuser/.claude/projects/test-project/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.jsonl",
  "cwd": "/home/testuser/projects/cc-trace",
  "hook_event_name": "PostToolUse",
  "tool_name": "TaskUpdate",
  "tool_input": {
    "taskId": "1",
    "status": "completed"
  },
  "tool_response": {
    "success": true,
    "taskId": "1",
    "updatedFields": ["status"]
  },
  "tool_use_id": "toolu_test_001"
}
```

**Step 2: Create `posttooluse_read.json`**

PostToolUse for a Read tool call with file path in input:

```json
{
  "session_id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
  "transcript_path": "/home/testuser/.claude/projects/test-project/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.jsonl",
  "cwd": "/home/testuser/projects/cc-trace",
  "hook_event_name": "PostToolUse",
  "tool_name": "Read",
  "tool_input": {
    "file_path": "/home/testuser/projects/cc-trace/main.go"
  },
  "tool_response": "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}",
  "tool_use_id": "toolu_test_002"
}
```

**Step 3: Create `posttooluse_failure.json`**

PostToolUseFailure event with error:

```json
{
  "session_id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
  "transcript_path": "/home/testuser/.claude/projects/test-project/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.jsonl",
  "cwd": "/home/testuser/projects/cc-trace",
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

**Step 4: Create `stop_simple.json`**

Stop event. The `transcript_path` will be overridden in tests to point at a fixture JSONL:

```json
{
  "session_id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
  "transcript_path": "WILL_BE_OVERRIDDEN_IN_TEST",
  "cwd": "/home/testuser/projects/cc-trace",
  "hook_event_name": "Stop",
  "stop_hook_active": false,
  "last_assistant_message": "Done. The build succeeded."
}
```

**Step 5: Create `subagent_stop.json`**

SubagentStop event. The `agent_transcript_path` will be overridden in tests:

```json
{
  "session_id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
  "transcript_path": "/home/testuser/.claude/projects/test-project/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.jsonl",
  "cwd": "/home/testuser/projects/cc-trace",
  "hook_event_name": "SubagentStop",
  "agent_id": "agent_test_001",
  "agent_type": "general-purpose",
  "agent_transcript_path": "WILL_BE_OVERRIDDEN_IN_TEST"
}
```

**Step 6: Commit hook input fixtures**

```bash
git add testdata/fixtures/posttooluse_taskupdate.json testdata/fixtures/posttooluse_read.json testdata/fixtures/posttooluse_failure.json testdata/fixtures/stop_simple.json testdata/fixtures/subagent_stop.json
git commit -m "Add hook input fixture files for testing" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Create Subagent Transcript and State Fixtures

**Files:**
- Create: `testdata/fixtures/transcript_subagent.jsonl`
- Create: `testdata/fixtures/state_with_tools.json`

**Step 1: Create `transcript_subagent.jsonl`**

A minimal subagent transcript (2 turns with one tool call). This is what `parseTranscript` receives when processing `agent_transcript_path`:

```jsonl
{"type":"user","sessionId":"sub-session-1111","cwd":"/home/testuser/projects/cc-trace","message":{"role":"user","content":[{"type":"text","text":"Search for the main function"}]},"timestamp":"2026-01-15T10:00:01.000Z","uuid":"su1"}
{"type":"assistant","sessionId":"sub-session-1111","message":{"role":"assistant","model":"claude-haiku-4-5-20251001","id":"msg_sub_001","type":"message","content":[{"type":"text","text":"Let me search for it."},{"type":"tool_use","id":"toolu_test_sub_001","name":"Grep","input":{"pattern":"func main","path":"/home/testuser/projects/cc-trace"}}],"stop_reason":"tool_use","usage":{"input_tokens":50,"output_tokens":15,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}},"timestamp":"2026-01-15T10:00:02.000Z","uuid":"sa1"}
{"type":"user","sessionId":"sub-session-1111","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_test_sub_001","content":"main.go:52:func main() {"}]},"timestamp":"2026-01-15T10:00:03.000Z","uuid":"str1"}
{"type":"assistant","sessionId":"sub-session-1111","message":{"role":"assistant","model":"claude-haiku-4-5-20251001","id":"msg_sub_002","type":"message","content":[{"type":"text","text":"Found it in main.go at line 52."}],"stop_reason":"end_turn","usage":{"input_tokens":80,"output_tokens":12,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}},"timestamp":"2026-01-15T10:00:04.000Z","uuid":"sa2"}
```

**Step 2: Create `state_with_tools.json`**

A pre-populated state file with tool span data and a pending subagent:

```json
{
  "sessions": {
    "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee": {
      "session_span_id": "",
      "session_start": "0001-01-01T00:00:00Z",
      "transcript_path": "/home/testuser/.claude/projects/test-project/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.jsonl",
      "cwd": "/home/testuser/projects/cc-trace",
      "last_line": 0,
      "turn_count": 0,
      "tool_spans": [
        {
          "tool_name": "Read",
          "tool_use_id": "toolu_test_001",
          "tool_input": {"file_path": "/home/testuser/projects/cc-trace/main.go"},
          "tool_response": "package main...",
          "timestamp": "2026-01-15T10:00:04.000Z"
        }
      ],
      "updated": "2026-01-15T10:00:05.000Z"
    }
  }
}
```

**Step 3: Commit subagent and state fixtures**

```bash
git add testdata/fixtures/transcript_subagent.jsonl testdata/fixtures/state_with_tools.json
git commit -m "Add subagent transcript and state fixture files" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 4: Refactor tracer.go for Testability

**Files:**
- Modify: `tracer.go:36-69` (refactor `initTracer`)

**Step 1: Write the failing test**

Create `tracer_test.go` with a test that calls `initTracerWithExporter`:

```go
package main

import (
	"testing"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestInitTracerWithExporter(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	shutdown, err := initTracerWithExporter(exporter)
	if err != nil {
		t.Fatalf("initTracerWithExporter failed: %v", err)
	}
	defer shutdown()

	// Verify exporter is connected by checking no panic.
	spans := exporter.GetSpans()
	if len(spans) != 0 {
		t.Errorf("expected 0 spans initially, got %d", len(spans))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestInitTracerWithExporter -v`
Expected: FAIL with "undefined: initTracerWithExporter"

**Step 3: Implement `initTracerWithExporter` in `tracer.go`**

Replace the existing `initTracer` function (lines 36-69) with:

```go
// initTracerWithExporter sets up the OTel TracerProvider with the given exporter.
func initTracerWithExporter(exporter sdktrace.SpanExporter) (func(), error) {
	ctx := context.Background()

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithAttributes(
			attribute.String("hook.version", "0.1.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tp.Shutdown(ctx)
	}
	return shutdown, nil
}

// initTracer sets up the OTel TracerProvider with OTLP HTTP exporter.
//
// All configuration is read from standard OTel environment variables:
//   - OTEL_EXPORTER_OTLP_TRACES_ENDPOINT or OTEL_EXPORTER_OTLP_ENDPOINT (default: http://localhost:4318)
//   - OTEL_SERVICE_NAME (default: unknown_service)
//   - OTEL_RESOURCE_ATTRIBUTES (default: none)
func initTracer() (func(), error) {
	ctx := context.Background()

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithInsecure(),
		otlptracehttp.WithTimeout(5*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("create exporter: %w", err)
	}

	return initTracerWithExporter(exporter)
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestInitTracerWithExporter -v`
Expected: PASS

**Step 5: Run full build to verify no regression**

Run: `go build ./...`
Expected: Success

**Step 6: Commit**

```bash
git add tracer.go tracer_test.go
git commit -m "Refactor initTracer to accept injectable exporter for testing" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 5: Write transcript_test.go — Parsing Layer

**Files:**
- Create: `transcript_test.go` (tests)
- Read: `testdata/fixtures/transcript_simple.jsonl`, `testdata/fixtures/transcript_tools.jsonl`

**Step 1: Write transcript parsing tests**

Create `transcript_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixtureDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata", "fixtures")
}

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(fixtureDir(t), name)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fixture not found: %s", p)
	}
	return p
}

func TestParseTranscript_Simple(t *testing.T) {
	turns, totalLines, err := parseTranscript(fixturePath(t, "transcript_simple.jsonl"), 0)
	if err != nil {
		t.Fatalf("parseTranscript failed: %v", err)
	}

	if totalLines != 4 {
		t.Errorf("expected 4 lines, got %d", totalLines)
	}
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(turns))
	}

	// Turn 1.
	if turns[0].UserText != "Hello, what is this project?" {
		t.Errorf("turn 1 user text = %q", turns[0].UserText)
	}
	if turns[0].Model != "claude-sonnet-4-20250514" {
		t.Errorf("turn 1 model = %q", turns[0].Model)
	}
	if turns[0].InputTokens != 100 {
		t.Errorf("turn 1 input tokens = %d", turns[0].InputTokens)
	}
	if turns[0].OutputTokens != 20 {
		t.Errorf("turn 1 output tokens = %d", turns[0].OutputTokens)
	}
	if turns[0].CacheReadTokens != 50 {
		t.Errorf("turn 1 cache read tokens = %d", turns[0].CacheReadTokens)
	}
	if turns[0].CacheCreationTokens != 10 {
		t.Errorf("turn 1 cache creation tokens = %d", turns[0].CacheCreationTokens)
	}
	if turns[0].StopReason != "end_turn" {
		t.Errorf("turn 1 stop reason = %q", turns[0].StopReason)
	}

	// Turn 2.
	if turns[1].UserText != "How does it work?" {
		t.Errorf("turn 2 user text = %q", turns[1].UserText)
	}
	if turns[1].Number != 2 {
		t.Errorf("turn 2 number = %d", turns[1].Number)
	}
}

func TestParseTranscript_ToolCalls(t *testing.T) {
	turns, _, err := parseTranscript(fixturePath(t, "transcript_tools.jsonl"), 0)
	if err != nil {
		t.Fatalf("parseTranscript failed: %v", err)
	}

	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	turn := turns[0]

	if len(turn.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(turn.ToolCalls))
	}

	// First tool call: Read.
	tc1 := turn.ToolCalls[0]
	if tc1.Name != "Read" {
		t.Errorf("tool 1 name = %q", tc1.Name)
	}
	if tc1.ID != "toolu_test_001" {
		t.Errorf("tool 1 id = %q", tc1.ID)
	}
	if !tc1.Success {
		t.Error("tool 1 should be success")
	}

	// Second tool call: Bash.
	tc2 := turn.ToolCalls[1]
	if tc2.Name != "Bash" {
		t.Errorf("tool 2 name = %q", tc2.Name)
	}
	if tc2.ID != "toolu_test_002" {
		t.Errorf("tool 2 id = %q", tc2.ID)
	}
}

func TestParseTranscript_DurationMs(t *testing.T) {
	turns, _, err := parseTranscript(fixturePath(t, "transcript_tools.jsonl"), 0)
	if err != nil {
		t.Fatalf("parseTranscript failed: %v", err)
	}

	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].DurationMs != 12000 {
		t.Errorf("expected durationMs=12000, got %d", turns[0].DurationMs)
	}
}

func TestParseTranscript_StartLine(t *testing.T) {
	// Parse from the middle of transcript_simple.jsonl (skip first 2 lines = turn 1).
	turns, totalLines, err := parseTranscript(fixturePath(t, "transcript_simple.jsonl"), 2)
	if err != nil {
		t.Fatalf("parseTranscript failed: %v", err)
	}

	if totalLines != 4 {
		t.Errorf("expected totalLines=4, got %d", totalLines)
	}
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn (second turn only), got %d", len(turns))
	}
	if turns[0].UserText != "How does it work?" {
		t.Errorf("expected second turn text, got %q", turns[0].UserText)
	}
}

func TestParseTranscript_StopReason(t *testing.T) {
	turns, _, err := parseTranscript(fixturePath(t, "transcript_tools.jsonl"), 0)
	if err != nil {
		t.Fatalf("parseTranscript failed: %v", err)
	}

	if len(turns) == 0 {
		t.Fatal("expected at least 1 turn")
	}
	// The last assistant message has stop_reason "end_turn".
	if turns[0].StopReason != "end_turn" {
		t.Errorf("expected stop_reason=end_turn, got %q", turns[0].StopReason)
	}
}

func TestExtractUsage(t *testing.T) {
	msg := map[string]interface{}{
		"message": map[string]interface{}{
			"usage": map[string]interface{}{
				"input_tokens":                float64(100),
				"output_tokens":               float64(50),
				"cache_read_input_tokens":      float64(30),
				"cache_creation_input_tokens":  float64(10),
			},
		},
	}

	input, output, cacheRead, cacheCreation := extractUsage(msg)
	if input != 100 {
		t.Errorf("input tokens = %d", input)
	}
	if output != 50 {
		t.Errorf("output tokens = %d", output)
	}
	if cacheRead != 30 {
		t.Errorf("cache read tokens = %d", cacheRead)
	}
	if cacheCreation != 10 {
		t.Errorf("cache creation tokens = %d", cacheCreation)
	}
}

func TestHelpers(t *testing.T) {
	t.Run("msgRole_user", func(t *testing.T) {
		msg := map[string]interface{}{"type": "user"}
		if got := msgRole(msg); got != "user" {
			t.Errorf("msgRole = %q, want user", got)
		}
	})

	t.Run("msgRole_assistant_nested", func(t *testing.T) {
		msg := map[string]interface{}{
			"message": map[string]interface{}{"role": "assistant"},
		}
		if got := msgRole(msg); got != "assistant" {
			t.Errorf("msgRole = %q, want assistant", got)
		}
	})

	t.Run("getTextContent_string", func(t *testing.T) {
		msg := map[string]interface{}{
			"content": "plain text",
		}
		if got := getTextContent(msg); got != "plain text" {
			t.Errorf("getTextContent = %q", got)
		}
	})

	t.Run("getTextContent_blocks", func(t *testing.T) {
		msg := map[string]interface{}{
			"message": map[string]interface{}{
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "hello"},
					map[string]interface{}{"type": "text", "text": "world"},
				},
			},
		}
		if got := getTextContent(msg); got != "hello\nworld" {
			t.Errorf("getTextContent = %q", got)
		}
	})

	t.Run("isToolResult_true", func(t *testing.T) {
		msg := map[string]interface{}{
			"message": map[string]interface{}{
				"content": []interface{}{
					map[string]interface{}{"type": "tool_result", "tool_use_id": "abc"},
				},
			},
		}
		if !isToolResult(msg) {
			t.Error("expected isToolResult=true")
		}
	})

	t.Run("isToolResult_false", func(t *testing.T) {
		msg := map[string]interface{}{
			"message": map[string]interface{}{
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "hello"},
				},
			},
		}
		if isToolResult(msg) {
			t.Error("expected isToolResult=false")
		}
	})

	t.Run("extractTimestamp_rfc3339", func(t *testing.T) {
		msg := map[string]interface{}{
			"timestamp": "2026-01-15T10:00:00.000Z",
		}
		ts := extractTimestamp(msg)
		expected := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
		if !ts.Equal(expected) {
			t.Errorf("extractTimestamp = %v, want %v", ts, expected)
		}
	})

	t.Run("extractModel", func(t *testing.T) {
		msg := map[string]interface{}{
			"message": map[string]interface{}{
				"model": "claude-sonnet-4-20250514",
			},
		}
		if got := extractModel(msg); got != "claude-sonnet-4-20250514" {
			t.Errorf("extractModel = %q", got)
		}
	})

	t.Run("extractStopReason", func(t *testing.T) {
		msg := map[string]interface{}{
			"message": map[string]interface{}{
				"stop_reason": "end_turn",
			},
		}
		if got := extractStopReason(msg); got != "end_turn" {
			t.Errorf("extractStopReason = %q", got)
		}
	})
}

func TestMergeAssistantParts(t *testing.T) {
	parts := []map[string]interface{}{
		{
			"message": map[string]interface{}{
				"id":      "msg_001",
				"role":    "assistant",
				"content": []interface{}{map[string]interface{}{"type": "text", "text": "part1"}},
			},
		},
		{
			"message": map[string]interface{}{
				"id":      "msg_001",
				"role":    "assistant",
				"content": []interface{}{map[string]interface{}{"type": "text", "text": "part2"}},
			},
		},
	}

	merged := mergeAssistantParts(parts)
	if merged == nil {
		t.Fatal("merged should not be nil")
	}

	text := getTextContent(merged)
	if text != "part1\npart2" {
		t.Errorf("merged text = %q, want 'part1\\npart2'", text)
	}
}
```

**Step 2: Run tests**

Run: `go test -run 'TestParseTranscript|TestExtractUsage|TestHelpers|TestMergeAssistantParts' -v`
Expected: All PASS

**Step 3: Commit**

```bash
git add transcript_test.go
git commit -m "Add transcript parsing tests with fixture-based assertions" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 6: Write state_test.go — State Layer

**Files:**
- Create: `state_test.go`

**Step 1: Write state management tests**

Create `state_test.go`:

```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	initStatePaths(dir)
	return dir
}

func TestLoadState_Empty(t *testing.T) {
	setupTestStateDir(t)
	sf := loadState()
	if sf == nil {
		t.Fatal("loadState returned nil")
	}
	if sf.Sessions == nil {
		t.Fatal("Sessions map is nil")
	}
	if len(sf.Sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sf.Sessions))
	}
}

func TestLoadState_Corrupt(t *testing.T) {
	dir := setupTestStateDir(t)
	stateDir := filepath.Join(dir, ".claude", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "otel_trace_state.json"), []byte("{corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}

	sf := loadState()
	if sf == nil {
		t.Fatal("loadState returned nil on corrupt file")
	}
	if len(sf.Sessions) != 0 {
		t.Errorf("expected 0 sessions on corrupt file, got %d", len(sf.Sessions))
	}
}

func TestSaveAndLoad(t *testing.T) {
	setupTestStateDir(t)

	sf := &StateFile{
		Sessions: map[string]*SessionState{
			"test-session": {
				TranscriptPath: "/tmp/test.jsonl",
				CWD:            "/tmp",
				LastLine:        42,
				TurnCount:       3,
				Updated:         time.Now(),
			},
		},
	}

	if err := saveState(sf); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}

	loaded := loadState()
	if len(loaded.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(loaded.Sessions))
	}

	ss := loaded.Sessions["test-session"]
	if ss == nil {
		t.Fatal("session 'test-session' not found")
	}
	if ss.LastLine != 42 {
		t.Errorf("LastLine = %d, want 42", ss.LastLine)
	}
	if ss.TurnCount != 3 {
		t.Errorf("TurnCount = %d, want 3", ss.TurnCount)
	}
}

func TestSaveState_PrunesStale(t *testing.T) {
	setupTestStateDir(t)

	sf := &StateFile{
		Sessions: map[string]*SessionState{
			"fresh": {
				Updated: time.Now(),
			},
			"stale": {
				Updated: time.Now().Add(-25 * time.Hour),
			},
		},
	}

	if err := saveState(sf); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}

	loaded := loadState()
	if _, ok := loaded.Sessions["fresh"]; !ok {
		t.Error("fresh session should be kept")
	}
	if _, ok := loaded.Sessions["stale"]; ok {
		t.Error("stale session should be pruned")
	}
}

func TestSaveAndLoad_WithToolSpans(t *testing.T) {
	setupTestStateDir(t)

	sf := &StateFile{
		Sessions: map[string]*SessionState{
			"test-session": {
				Updated: time.Now(),
				ToolSpans: []ToolSpanData{
					{
						ToolName:  "Read",
						ToolUseID: "toolu_test_001",
						ToolInput: map[string]interface{}{"file_path": "/tmp/test.go"},
						Timestamp: time.Now(),
					},
				},
			},
		},
	}

	if err := saveState(sf); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}

	loaded := loadState()
	ss := loaded.Sessions["test-session"]
	if len(ss.ToolSpans) != 1 {
		t.Fatalf("expected 1 tool span, got %d", len(ss.ToolSpans))
	}
	if ss.ToolSpans[0].ToolName != "Read" {
		t.Errorf("tool name = %q", ss.ToolSpans[0].ToolName)
	}
}

func TestLocking(t *testing.T) {
	setupTestStateDir(t)

	if !acquireLock() {
		t.Fatal("first acquireLock should succeed")
	}

	if acquireLock() {
		t.Fatal("second acquireLock should fail while lock held")
	}

	releaseLock()

	if !acquireLock() {
		t.Fatal("acquireLock after release should succeed")
	}
	releaseLock()
}

func TestStaleLockRemoval(t *testing.T) {
	dir := setupTestStateDir(t)

	// Create a lock file with an old modification time.
	lockPath := filepath.Join(dir, ".claude", "state", "otel_trace_state.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(lockPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// acquireLock should remove stale lock and succeed.
	if !acquireLock() {
		t.Fatal("acquireLock should succeed with stale lock")
	}
	releaseLock()
}

func TestLoadState_FromFixture(t *testing.T) {
	// Load the fixture state file and verify it parses correctly.
	data, err := os.ReadFile(fixturePath(t, "state_with_tools.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var sf StateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	ss := sf.Sessions["aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"]
	if ss == nil {
		t.Fatal("expected session not found in fixture")
	}
	if len(ss.ToolSpans) != 1 {
		t.Fatalf("expected 1 tool span, got %d", len(ss.ToolSpans))
	}
	if ss.ToolSpans[0].ToolName != "Read" {
		t.Errorf("tool name = %q", ss.ToolSpans[0].ToolName)
	}
}
```

**Step 2: Run tests**

Run: `go test -run 'TestLoadState|TestSaveAndLoad|TestSaveState|TestLocking|TestStaleLock' -v`
Expected: All PASS

**Step 3: Commit**

```bash
git add state_test.go
git commit -m "Add state management tests with temp dir isolation" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 7: Write tracer_test.go — Span Export Layer

**Files:**
- Modify: `tracer_test.go` (add to existing file from Task 4)

**Step 1: Write span export tests**

Append to `tracer_test.go` (which already has `TestInitTracerWithExporter`):

```go
package main

import (
	"os"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// setupTestTracer creates an InMemoryExporter and wires it as the global tracer.
// Returns the exporter and a cleanup function.
func setupTestTracer(t *testing.T) (*tracetest.InMemoryExporter, func()) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	shutdown, err := initTracerWithExporter(exporter)
	if err != nil {
		t.Fatalf("initTracerWithExporter: %v", err)
	}
	return exporter, shutdown
}

// findSpan finds a span by name in the exported spans.
func findSpan(spans tracetest.SpanStubs, name string) *tracetest.SpanStub {
	for i := range spans {
		if spans[i].Name == name {
			return &spans[i]
		}
	}
	return nil
}

// findSpans finds all spans matching a name prefix.
func findSpans(spans tracetest.SpanStubs, prefix string) []tracetest.SpanStub {
	var result []tracetest.SpanStub
	for _, s := range spans {
		if len(s.Name) >= len(prefix) && s.Name[:len(prefix)] == prefix {
			result = append(result, s)
		}
	}
	return result
}

// getAttr finds an attribute value by key.
func getAttr(span *tracetest.SpanStub, key string) interface{} {
	for _, a := range span.Attributes {
		if string(a.Key) == key {
			return a.Value.AsInterface()
		}
	}
	return nil
}

func TestInitTracerWithExporter(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	shutdown, err := initTracerWithExporter(exporter)
	if err != nil {
		t.Fatalf("initTracerWithExporter failed: %v", err)
	}
	defer shutdown()

	spans := exporter.GetSpans()
	if len(spans) != 0 {
		t.Errorf("expected 0 spans initially, got %d", len(spans))
	}
}

func TestTraceIDDeterministic(t *testing.T) {
	id1 := traceIDFromSession("test-session-123")
	id2 := traceIDFromSession("test-session-123")
	if id1 != id2 {
		t.Errorf("trace IDs differ: %s vs %s", id1, id2)
	}

	id3 := traceIDFromSession("different-session")
	if id1 == id3 {
		t.Error("different sessions should produce different trace IDs")
	}
}

func TestTraceparentParsing(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		t.Setenv("TRACEPARENT", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
		ctx, ok := parseTraceparent()
		if !ok {
			t.Fatal("expected valid TRACEPARENT")
		}
		if ctx == nil {
			t.Fatal("ctx should not be nil")
		}
	})

	t.Run("missing", func(t *testing.T) {
		t.Setenv("TRACEPARENT", "")
		_, ok := parseTraceparent()
		if ok {
			t.Error("expected invalid for empty TRACEPARENT")
		}
	})

	t.Run("invalid_format", func(t *testing.T) {
		t.Setenv("TRACEPARENT", "not-a-valid-traceparent")
		_, ok := parseTraceparent()
		if ok {
			t.Error("expected invalid for bad format")
		}
	})
}

func TestExportSessionTrace_SingleTurn(t *testing.T) {
	exporter, shutdown := setupTestTracer(t)
	defer shutdown()

	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	turns := []Turn{
		{
			Number:       1,
			UserText:     "Hello",
			Model:        "claude-sonnet-4-20250514",
			InputTokens:  100,
			OutputTokens: 20,
			StopReason:   "end_turn",
			StartTime:    now,
			EndTime:      now.Add(5 * time.Second),
			UserTimestamp: now,
		},
	}

	ss := &SessionState{}
	exportSessionTrace("test-session-1", turns, nil, nil, ss)

	spans := exporter.GetSpans()

	// Expect: Session span, Turn 1 span, LLM Response span = 3 spans.
	if len(spans) != 3 {
		t.Fatalf("expected 3 spans, got %d", len(spans))
	}

	sessionSpan := findSpan(spans, "Session test-sessi")
	if sessionSpan == nil {
		// Try matching by prefix.
		for _, s := range spans {
			t.Logf("span: %q", s.Name)
		}
		t.Fatal("Session span not found")
	}

	turnSpan := findSpan(spans, "Turn 1")
	if turnSpan == nil {
		t.Fatal("Turn 1 span not found")
	}

	llmSpan := findSpan(spans, "LLM Response")
	if llmSpan == nil {
		t.Fatal("LLM Response span not found")
	}

	// Verify parent relationships.
	if turnSpan.Parent.SpanID() != sessionSpan.SpanContext.SpanID() {
		t.Error("Turn 1 should be child of Session")
	}
	if llmSpan.Parent.SpanID() != turnSpan.SpanContext.SpanID() {
		t.Error("LLM Response should be child of Turn 1")
	}

	// Verify session span attributes.
	if v := getAttr(sessionSpan, "session.id"); v != "test-session-1" {
		t.Errorf("session.id = %v", v)
	}

	// Verify LLM span attributes.
	if v := getAttr(llmSpan, "gen_ai.request.model"); v != "claude-sonnet-4-20250514" {
		t.Errorf("gen_ai.request.model = %v", v)
	}
	if v := getAttr(llmSpan, "gen_ai.usage.input_tokens"); v != int64(100) {
		t.Errorf("gen_ai.usage.input_tokens = %v", v)
	}
	if v := getAttr(llmSpan, "gen_ai.response.finish_reason"); v != "end_turn" {
		t.Errorf("gen_ai.response.finish_reason = %v", v)
	}

	// Verify session span ID was persisted.
	if ss.SessionSpanID == "" {
		t.Error("SessionSpanID should be set after export")
	}
}

func TestExportSessionTrace_WithTools(t *testing.T) {
	exporter, shutdown := setupTestTracer(t)
	defer shutdown()

	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	turns := []Turn{
		{
			Number:       1,
			UserText:     "Read a file",
			Model:        "claude-sonnet-4-20250514",
			InputTokens:  100,
			OutputTokens: 20,
			StopReason:   "end_turn",
			StartTime:    now,
			EndTime:      now.Add(10 * time.Second),
			UserTimestamp: now,
			ToolCalls: []ToolCall{
				{
					Name:      "Read",
					ID:        "toolu_test_001",
					Input:     map[string]interface{}{"file_path": "/tmp/test.go"},
					Output:    "package main",
					StartTime: now.Add(1 * time.Second),
					EndTime:   now.Add(2 * time.Second),
					Success:   true,
				},
			},
		},
	}

	ss := &SessionState{}
	exportSessionTrace("test-session-2", turns, nil, nil, ss)

	spans := exporter.GetSpans()

	toolSpan := findSpan(spans, "Tool: Read")
	if toolSpan == nil {
		t.Fatal("Tool: Read span not found")
	}

	turnSpan := findSpan(spans, "Turn 1")
	if turnSpan == nil {
		t.Fatal("Turn 1 span not found")
	}

	// Tool should be child of Turn.
	if toolSpan.Parent.SpanID() != turnSpan.SpanContext.SpanID() {
		t.Error("Tool: Read should be child of Turn 1")
	}

	if v := getAttr(toolSpan, "tool.name"); v != "Read" {
		t.Errorf("tool.name = %v", v)
	}
	if v := getAttr(toolSpan, "tool.success"); v != true {
		t.Errorf("tool.success = %v", v)
	}
}

func TestExportSessionTrace_WithSubagent(t *testing.T) {
	exporter, shutdown := setupTestTracer(t)
	defer shutdown()

	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	turns := []Turn{
		{
			Number:       1,
			UserText:     "Do a task",
			Model:        "claude-sonnet-4-20250514",
			InputTokens:  100,
			OutputTokens: 20,
			StopReason:   "end_turn",
			StartTime:    now,
			EndTime:      now.Add(30 * time.Second),
			UserTimestamp: now,
			ToolCalls: []ToolCall{
				{
					Name:      "Task",
					ID:        "toolu_test_task",
					StartTime: now.Add(1 * time.Second),
					EndTime:   now.Add(25 * time.Second),
					Success:   true,
				},
			},
		},
	}

	subagents := []PendingSubagent{
		{
			AgentID:   "agent_test_001",
			AgentType: "general-purpose",
			Turns: []Turn{
				{
					Number:       1,
					UserText:     "Search",
					Model:        "claude-haiku-4-5-20251001",
					InputTokens:  50,
					OutputTokens: 15,
					StopReason:   "end_turn",
					StartTime:    now.Add(2 * time.Second),
					EndTime:      now.Add(20 * time.Second),
					UserTimestamp: now.Add(2 * time.Second),
				},
			},
		},
	}

	ss := &SessionState{}
	exportSessionTrace("test-session-3", turns, nil, subagents, ss)

	spans := exporter.GetSpans()

	subSpan := findSpan(spans, "Subagent: general-purpose")
	if subSpan == nil {
		for _, s := range spans {
			t.Logf("span: %q", s.Name)
		}
		t.Fatal("Subagent span not found")
	}

	if v := getAttr(subSpan, "agent.type"); v != "general-purpose" {
		t.Errorf("agent.type = %v", v)
	}
	if v := getAttr(subSpan, "agent.id"); v != "agent_test_001" {
		t.Errorf("agent.id = %v", v)
	}
}

func TestExportSessionTrace_IncrementalExport(t *testing.T) {
	exporter, shutdown := setupTestTracer(t)
	defer shutdown()

	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

	// First export: creates Session span.
	turns1 := []Turn{
		{
			Number: 1, UserText: "First", Model: "claude-sonnet-4-20250514",
			InputTokens: 100, OutputTokens: 20, StopReason: "end_turn",
			StartTime: now, EndTime: now.Add(5 * time.Second), UserTimestamp: now,
		},
	}

	ss := &SessionState{}
	exportSessionTrace("test-session-inc", turns1, nil, nil, ss)

	savedSpanID := ss.SessionSpanID
	if savedSpanID == "" {
		t.Fatal("SessionSpanID should be set")
	}

	exporter.Reset()

	// Second export: reuses Session span ID (no new Session span).
	turns2 := []Turn{
		{
			Number: 2, UserText: "Second", Model: "claude-sonnet-4-20250514",
			InputTokens: 150, OutputTokens: 25, StopReason: "end_turn",
			StartTime: now.Add(10 * time.Second), EndTime: now.Add(15 * time.Second),
			UserTimestamp: now.Add(10 * time.Second),
		},
	}

	exportSessionTrace("test-session-inc", turns2, nil, nil, ss)

	if ss.SessionSpanID != savedSpanID {
		t.Error("SessionSpanID should remain the same on incremental export")
	}

	spans := exporter.GetSpans()
	// Should have Turn 2 + LLM Response = 2 spans (no new Session span).
	sessionSpans := findSpans(spans, "Session")
	if len(sessionSpans) != 0 {
		t.Errorf("expected 0 new Session spans on incremental export, got %d", len(sessionSpans))
	}

	turnSpan := findSpan(spans, "Turn 2")
	if turnSpan == nil {
		t.Fatal("Turn 2 span not found")
	}
}

func TestExportSessionTrace_SpanAttributes(t *testing.T) {
	exporter, shutdown := setupTestTracer(t)
	defer shutdown()

	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	turns := []Turn{
		{
			Number: 1, UserText: "Test attributes", Model: "claude-sonnet-4-20250514",
			InputTokens: 100, OutputTokens: 20, StopReason: "end_turn",
			DurationMs: 5000, CacheReadTokens: 50, CacheCreationTokens: 10,
			StartTime: now, EndTime: now.Add(5 * time.Second), UserTimestamp: now,
		},
	}

	ss := &SessionState{}
	exportSessionTrace("test-session-attrs", turns, nil, nil, ss)

	spans := exporter.GetSpans()

	turnSpan := findSpan(spans, "Turn 1")
	if turnSpan == nil {
		t.Fatal("Turn 1 not found")
	}

	// Check turn-level attributes.
	if v := getAttr(turnSpan, "turn.number"); v != int64(1) {
		t.Errorf("turn.number = %v", v)
	}
	if v := getAttr(turnSpan, "turn.duration_ms"); v != int64(5000) {
		t.Errorf("turn.duration_ms = %v", v)
	}
	if v := getAttr(turnSpan, "turn.stop_reason"); v != "end_turn" {
		t.Errorf("turn.stop_reason = %v", v)
	}

	llmSpan := findSpan(spans, "LLM Response")
	if llmSpan == nil {
		t.Fatal("LLM Response not found")
	}

	if v := getAttr(llmSpan, "gen_ai.system"); v != "anthropic" {
		t.Errorf("gen_ai.system = %v", v)
	}
	if v := getAttr(llmSpan, "gen_ai.usage.cache_read_tokens"); v != int64(50) {
		t.Errorf("gen_ai.usage.cache_read_tokens = %v", v)
	}
	if v := getAttr(llmSpan, "gen_ai.usage.cache_creation_tokens"); v != int64(10) {
		t.Errorf("gen_ai.usage.cache_creation_tokens = %v", v)
	}
}
```

**Step 2: Run tests**

Run: `go test -run 'TestInitTracer|TestTraceID|TestTraceparent|TestExportSession' -v`
Expected: All PASS

**Step 3: Commit**

```bash
git add tracer_test.go
git commit -m "Add OTel span export tests with InMemoryExporter" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 8: Write main_test.go — Integration Layer

**Files:**
- Create: `main_test.go`

**Step 1: Write integration tests**

Create `main_test.go`:

```go
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// loadFixtureInput reads a fixture JSON file and unmarshals it into HookInput.
func loadFixtureInput(t *testing.T, name string) HookInput {
	t.Helper()
	data, err := os.ReadFile(fixturePath(t, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var input HookInput
	if err := json.Unmarshal(data, &input); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", name, err)
	}
	return input
}

func TestHandlePostToolUse_Integration(t *testing.T) {
	setupTestStateDir(t)

	input := loadFixtureInput(t, "posttooluse_taskupdate.json")
	handlePostToolUse(input)

	sf := loadState()
	ss, ok := sf.Sessions[input.SessionID]
	if !ok {
		t.Fatal("session not found in state after PostToolUse")
	}
	if len(ss.ToolSpans) != 1 {
		t.Fatalf("expected 1 tool span, got %d", len(ss.ToolSpans))
	}
	if ss.ToolSpans[0].ToolName != "TaskUpdate" {
		t.Errorf("tool name = %q, want TaskUpdate", ss.ToolSpans[0].ToolName)
	}
	if ss.ToolSpans[0].ToolUseID != "toolu_test_001" {
		t.Errorf("tool use id = %q", ss.ToolSpans[0].ToolUseID)
	}
}

func TestHandlePostToolUse_Read(t *testing.T) {
	setupTestStateDir(t)

	input := loadFixtureInput(t, "posttooluse_read.json")
	handlePostToolUse(input)

	sf := loadState()
	ss := sf.Sessions[input.SessionID]
	if ss == nil {
		t.Fatal("session not found")
	}
	if ss.ToolSpans[0].ToolName != "Read" {
		t.Errorf("tool name = %q", ss.ToolSpans[0].ToolName)
	}
}

func TestHandlePostToolUse_Failure(t *testing.T) {
	setupTestStateDir(t)

	// PostToolUseFailure also goes through handlePostToolUse.
	input := loadFixtureInput(t, "posttooluse_failure.json")
	handlePostToolUse(input)

	sf := loadState()
	ss := sf.Sessions[input.SessionID]
	if ss == nil {
		t.Fatal("session not found")
	}
	if ss.ToolSpans[0].ToolName != "Bash" {
		t.Errorf("tool name = %q", ss.ToolSpans[0].ToolName)
	}
}

func TestHandleSubagentStop_Integration(t *testing.T) {
	dir := setupTestStateDir(t)

	// Copy subagent transcript fixture to a temp file the handler can read.
	src := fixturePath(t, "transcript_subagent.jsonl")
	dst := filepath.Join(dir, "subagent.jsonl")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}

	input := loadFixtureInput(t, "subagent_stop.json")
	input.AgentTranscriptPath = dst

	handleSubagentStop(input)

	sf := loadState()
	ss := sf.Sessions[input.SessionID]
	if ss == nil {
		t.Fatal("session not found")
	}
	if len(ss.PendingSubagents) != 1 {
		t.Fatalf("expected 1 pending subagent, got %d", len(ss.PendingSubagents))
	}
	if ss.PendingSubagents[0].AgentType != "general-purpose" {
		t.Errorf("agent type = %q", ss.PendingSubagents[0].AgentType)
	}
	if len(ss.PendingSubagents[0].Turns) == 0 {
		t.Error("expected subagent turns to be parsed")
	}
}

func TestHandleStop_Integration(t *testing.T) {
	dir := setupTestStateDir(t)

	exporter, shutdown := setupTestTracer(t)
	defer shutdown()

	// Copy transcript fixture to temp dir.
	src := fixturePath(t, "transcript_simple.jsonl")
	dst := filepath.Join(dir, "transcript.jsonl")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}

	input := loadFixtureInput(t, "stop_simple.json")
	input.TranscriptPath = dst

	handleStop(input)

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected spans to be exported")
	}

	// Should have Session + 2 Turns + 2 LLM Responses = 5 spans.
	if len(spans) != 5 {
		t.Errorf("expected 5 spans, got %d", len(spans))
		for _, s := range spans {
			t.Logf("  span: %q", s.Name)
		}
	}

	// Verify state was updated.
	sf := loadState()
	ss := sf.Sessions[input.SessionID]
	if ss == nil {
		t.Fatal("session not found after Stop")
	}
	if ss.TurnCount != 2 {
		t.Errorf("TurnCount = %d, want 2", ss.TurnCount)
	}
	if ss.LastLine != 4 {
		t.Errorf("LastLine = %d, want 4", ss.LastLine)
	}
}

func TestFullFlow(t *testing.T) {
	dir := setupTestStateDir(t)

	// Step 1: PostToolUse — record a tool call.
	postToolInput := loadFixtureInput(t, "posttooluse_read.json")
	handlePostToolUse(postToolInput)

	// Step 2: SubagentStop — record subagent.
	subSrc := fixturePath(t, "transcript_subagent.jsonl")
	subDst := filepath.Join(dir, "subagent.jsonl")
	subData, _ := os.ReadFile(subSrc)
	_ = os.WriteFile(subDst, subData, 0o644)

	subInput := loadFixtureInput(t, "subagent_stop.json")
	subInput.AgentTranscriptPath = subDst
	handleSubagentStop(subInput)

	// Step 3: Stop — export everything.
	exporter, shutdown := setupTestTracer(t)
	defer shutdown()

	transSrc := fixturePath(t, "transcript_tools.jsonl")
	transDst := filepath.Join(dir, "transcript.jsonl")
	transData, _ := os.ReadFile(transSrc)
	_ = os.WriteFile(transDst, transData, 0o644)

	stopInput := loadFixtureInput(t, "stop_simple.json")
	stopInput.TranscriptPath = transDst
	handleStop(stopInput)

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected spans after full flow")
	}

	// Verify we have tool enrichment from PostToolUse data.
	// The handleStop merges ToolSpans from state with transcript tool calls.
	sf := loadState()
	ss := sf.Sessions[stopInput.SessionID]
	if ss == nil {
		t.Fatal("session not found")
	}
	// Tool spans should be cleared after export.
	if len(ss.ToolSpans) != 0 {
		t.Errorf("tool spans should be cleared after Stop, got %d", len(ss.ToolSpans))
	}
	// Pending subagents should be cleared after export.
	if len(ss.PendingSubagents) != 0 {
		t.Errorf("pending subagents should be cleared after Stop, got %d", len(ss.PendingSubagents))
	}
}
```

**Step 2: Run tests**

Run: `go test -run 'TestHandlePostToolUse|TestHandleSubagentStop|TestHandleStop|TestFullFlow' -v`
Expected: All PASS

**Step 3: Commit**

```bash
git add main_test.go
git commit -m "Add integration tests for hook handlers and full flow" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 9: Run Full Test Suite and Verify

**Step 1: Run all tests**

Run: `go test -v -count=1 ./...`
Expected: All tests pass

**Step 2: Run with race detector**

Run: `go test -race -v -count=1 ./...`
Expected: No race conditions detected, all pass

**Step 3: Check test coverage**

Run: `go test -cover ./...`
Expected: Coverage report showing coverage across all files

**Step 4: Commit any test fixes**

If any tests needed adjustments, commit those fixes.

---

### Task 10: Add Test Target to Makefile and Final Commit

**Files:**
- Modify: `Makefile`

**Step 1: Add test target to Makefile**

Add after the `clean` target:

```makefile
test:
	go test -v -count=1 ./...

test-race:
	go test -race -v -count=1 ./...

test-cover:
	go test -cover ./...
```

Update the `.PHONY` line to include the new targets:

```makefile
.PHONY: build install clean test test-race test-cover
```

**Step 2: Run `make test` to verify**

Run: `make test`
Expected: All tests pass

**Step 3: Commit**

```bash
git add Makefile
git commit -m "Add test targets to Makefile" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```
