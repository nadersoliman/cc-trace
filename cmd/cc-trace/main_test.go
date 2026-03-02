package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"cc-trace/internal/hook"
	"cc-trace/internal/logging"
	"cc-trace/internal/state"
)

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("..", "..", "testdata", "fixtures", name)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fixture not found: %s", p)
	}
	return p
}

// loadFixtureInput reads a JSON fixture file and unmarshals it into a HookInput.
func loadFixtureInput(t *testing.T, name string) hook.HookInput {
	t.Helper()
	data, err := os.ReadFile(fixturePath(t, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var input hook.HookInput
	if err := json.Unmarshal(data, &input); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", name, err)
	}
	return input
}

// copyFixtureToDir copies a fixture file into a target directory, returning the new path.
func copyFixtureToDir(t *testing.T, fixtureName, dir string) string {
	t.Helper()
	src := fixturePath(t, fixtureName)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixtureName, err)
	}
	dst := filepath.Join(dir, fixtureName)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write fixture copy to %s: %v", dst, err)
	}
	return dst
}

func setupTestStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	state.Init(dir)
	// Create the state directory so lock/state files can be written.
	stateDir := filepath.Join(dir, ".claude", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("failed to create state dir: %v", err)
	}
	// Initialise logging so Debug calls don't fail.
	logFile := filepath.Join(dir, "test.log")
	logging.Init(logFile, false)
	// Prevent dump calls during tests.
	dumpEnabled = false
	return dir
}

// --- Integration Tests ---

func TestHandlePostToolUse_Integration(t *testing.T) {
	setupTestStateDir(t)

	input := loadFixtureInput(t, "posttooluse_taskupdate.json")
	handlePostToolUse(input)

	sf := state.LoadState()
	ss, ok := sf.Sessions[input.SessionID]
	if !ok {
		t.Fatalf("session %q not found in state after handlePostToolUse", input.SessionID)
	}
	if len(ss.ToolSpans) != 1 {
		t.Fatalf("expected 1 ToolSpan, got %d", len(ss.ToolSpans))
	}
	if ss.ToolSpans[0].ToolName != "TaskUpdate" {
		t.Errorf("ToolName = %q, want %q", ss.ToolSpans[0].ToolName, "TaskUpdate")
	}
	if ss.ToolSpans[0].ToolUseID != "toolu_test_001" {
		t.Errorf("ToolUseID = %q, want %q", ss.ToolSpans[0].ToolUseID, "toolu_test_001")
	}
}

func TestHandlePostToolUse_Read(t *testing.T) {
	setupTestStateDir(t)

	input := loadFixtureInput(t, "posttooluse_read.json")
	handlePostToolUse(input)

	sf := state.LoadState()
	ss, ok := sf.Sessions[input.SessionID]
	if !ok {
		t.Fatalf("session %q not found in state after handlePostToolUse", input.SessionID)
	}
	if len(ss.ToolSpans) != 1 {
		t.Fatalf("expected 1 ToolSpan, got %d", len(ss.ToolSpans))
	}
	if ss.ToolSpans[0].ToolName != "Read" {
		t.Errorf("ToolName = %q, want %q", ss.ToolSpans[0].ToolName, "Read")
	}
}

func TestHandlePostToolUse_Failure(t *testing.T) {
	setupTestStateDir(t)

	input := loadFixtureInput(t, "posttooluse_failure.json")
	// PostToolUseFailure routes through handlePostToolUse in main.go's switch.
	handlePostToolUse(input)

	sf := state.LoadState()
	ss, ok := sf.Sessions[input.SessionID]
	if !ok {
		t.Fatalf("session %q not found in state after handlePostToolUse (failure)", input.SessionID)
	}
	if len(ss.ToolSpans) != 1 {
		t.Fatalf("expected 1 ToolSpan, got %d", len(ss.ToolSpans))
	}
	if ss.ToolSpans[0].ToolName != "Bash" {
		t.Errorf("ToolName = %q, want %q", ss.ToolSpans[0].ToolName, "Bash")
	}
}

func TestHandleSubagentStop_Integration(t *testing.T) {
	setupTestStateDir(t)

	// Copy subagent transcript to a temp directory so handleSubagentStop can read it.
	tmpDir := t.TempDir()
	transcriptPath := copyFixtureToDir(t, "transcript_subagent.jsonl", tmpDir)

	input := loadFixtureInput(t, "subagent_stop.json")
	input.AgentTranscriptPath = transcriptPath

	handleSubagentStop(input)

	sf := state.LoadState()
	ss, ok := sf.Sessions[input.SessionID]
	if !ok {
		t.Fatalf("session %q not found in state after handleSubagentStop", input.SessionID)
	}
	if len(ss.PendingSubagents) != 1 {
		t.Fatalf("expected 1 PendingSubagent, got %d", len(ss.PendingSubagents))
	}
	if ss.PendingSubagents[0].AgentType != "general-purpose" {
		t.Errorf("AgentType = %q, want %q", ss.PendingSubagents[0].AgentType, "general-purpose")
	}
	if len(ss.PendingSubagents[0].Turns) == 0 {
		t.Error("PendingSubagent has 0 turns, expected non-empty")
	}
}

func TestHandleStop_Integration(t *testing.T) {
	setupTestStateDir(t)

	// Point OTLP exporter at an unreachable port so initTracer's HTTP exporter
	// creation succeeds but export fails fast (no 5-second timeout hanging).
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:1")

	// Copy transcript to a temp directory.
	tmpDir := t.TempDir()
	transcriptPath := copyFixtureToDir(t, "transcript_simple.jsonl", tmpDir)

	input := loadFixtureInput(t, "stop_simple.json")
	input.TranscriptPath = transcriptPath

	handleStop(input)

	// Verify state side effects: TurnCount and LastLine updated.
	sf := state.LoadState()
	ss, ok := sf.Sessions[input.SessionID]
	if !ok {
		t.Fatalf("session %q not found in state after handleStop", input.SessionID)
	}

	// transcript_simple.jsonl has 4 lines and 2 turns.
	if ss.LastLine != 4 {
		t.Errorf("LastLine = %d, want 4", ss.LastLine)
	}
	if ss.TurnCount != 2 {
		t.Errorf("TurnCount = %d, want 2", ss.TurnCount)
	}
}

func TestFullFlow(t *testing.T) {
	setupTestStateDir(t)

	sessionID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	// Step 1: handlePostToolUse with a Read tool event.
	readInput := loadFixtureInput(t, "posttooluse_read.json")
	handlePostToolUse(readInput)

	// Verify tool was recorded.
	sf := state.LoadState()
	ss, ok := sf.Sessions[sessionID]
	if !ok {
		t.Fatalf("session %q not found after PostToolUse", sessionID)
	}
	if len(ss.ToolSpans) != 1 {
		t.Fatalf("expected 1 ToolSpan after PostToolUse, got %d", len(ss.ToolSpans))
	}

	// Step 2: handleSubagentStop with subagent transcript.
	tmpDir := t.TempDir()
	subagentTranscript := copyFixtureToDir(t, "transcript_subagent.jsonl", tmpDir)
	subagentInput := loadFixtureInput(t, "subagent_stop.json")
	subagentInput.AgentTranscriptPath = subagentTranscript
	handleSubagentStop(subagentInput)

	// Verify subagent was recorded.
	sf = state.LoadState()
	ss = sf.Sessions[sessionID]
	if len(ss.PendingSubagents) != 1 {
		t.Fatalf("expected 1 PendingSubagent after SubagentStop, got %d", len(ss.PendingSubagents))
	}

	// Step 3: handleStop -- use setupTestTracer so we can inspect exported spans.
	// Note: handleStop calls tracer.InitTracer() internally which overrides the global
	// TracerProvider. To capture spans, we set OTEL_EXPORTER_OTLP_ENDPOINT to
	// an unreachable port and verify state side effects instead.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:1")

	toolsTranscript := copyFixtureToDir(t, "transcript_tools.jsonl", tmpDir)
	stopInput := loadFixtureInput(t, "stop_simple.json")
	stopInput.TranscriptPath = toolsTranscript
	handleStop(stopInput)

	// Verify state side effects after Stop.
	sf = state.LoadState()
	ss, ok = sf.Sessions[sessionID]
	if !ok {
		t.Fatalf("session %q not found after Stop", sessionID)
	}

	// ToolSpans should be cleared after export.
	if len(ss.ToolSpans) != 0 {
		t.Errorf("ToolSpans should be cleared after Stop, got %d", len(ss.ToolSpans))
	}

	// PendingSubagents should be cleared after export.
	if len(ss.PendingSubagents) != 0 {
		t.Errorf("PendingSubagents should be cleared after Stop, got %d", len(ss.PendingSubagents))
	}

	// transcript_tools.jsonl has 7 lines and 1 turn (with tools).
	if ss.TurnCount != 1 {
		t.Errorf("TurnCount = %d, want 1", ss.TurnCount)
	}
	if ss.LastLine != 7 {
		t.Errorf("LastLine = %d, want 7", ss.LastLine)
	}
}
