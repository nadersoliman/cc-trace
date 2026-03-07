package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// loadFixture reads a JSON fixture file and unmarshals it into the given type.
func loadFixture[T any](t *testing.T, name string) T {
	t.Helper()
	data, err := os.ReadFile(fixturePath(t, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var input T
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
	logging.InitTiming(false)
	// Prevent dump calls during tests.
	dumpEnabled = false
	return dir
}

// --- Integration Tests ---

func TestHandlePostToolUse_Integration(t *testing.T) {
	setupTestStateDir(t)

	input := loadFixture[hook.PostToolUsePayload](t, "posttooluse_taskupdate.json")
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

	input := loadFixture[hook.PostToolUsePayload](t, "posttooluse_read.json")
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

func TestHandlePostToolUseFailure_Integration(t *testing.T) {
	setupTestStateDir(t)

	input := loadFixture[hook.PostToolUseFailurePayload](t, "posttooluse_failure.json")
	handlePostToolUseFailure(input)

	sf := state.LoadState()
	ss, ok := sf.Sessions[input.SessionID]
	if !ok {
		t.Fatalf("session %q not found in state after handlePostToolUseFailure", input.SessionID)
	}
	if len(ss.ToolSpans) != 1 {
		t.Fatalf("expected 1 ToolSpan, got %d", len(ss.ToolSpans))
	}
	if ss.ToolSpans[0].ToolName != "Bash" {
		t.Errorf("ToolName = %q, want %q", ss.ToolSpans[0].ToolName, "Bash")
	}
}

func TestHandlePostToolUseFailure_StoresFailureFields(t *testing.T) {
	setupTestStateDir(t)

	input := loadFixture[hook.PostToolUseFailurePayload](t, "posttooluse_failure.json")
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
	if tsd.PermissionMode != "default" {
		t.Errorf("PermissionMode = %q, want %q", tsd.PermissionMode, "default")
	}
}

func TestHandleSubagentStop_Integration(t *testing.T) {
	setupTestStateDir(t)

	// Copy subagent transcript to a temp directory so handleSubagentStop can read it.
	tmpDir := t.TempDir()
	transcriptPath := copyFixtureToDir(t, "transcript_subagent.jsonl", tmpDir)

	input := loadFixture[hook.SubagentStopPayload](t, "subagent_stop.json")
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

	input := loadFixture[hook.StopPayload](t, "stop_simple.json")
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

func TestHandlePostToolUse_TimingOutput(t *testing.T) {
	dir := setupTestStateDir(t)
	logging.InitTiming(true)

	input := loadFixture[hook.PostToolUsePayload](t, "posttooluse_read.json")
	handlePostToolUse(input)

	data, err := os.ReadFile(filepath.Join(dir, "test.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "[TIMING]") {
		t.Errorf("expected [TIMING] in log output, got: %s", content)
	}
	if !strings.Contains(content, "PostToolUse") {
		t.Errorf("expected PostToolUse in timing log, got: %s", content)
	}
	if !strings.Contains(content, "total=") {
		t.Errorf("expected total= in timing log, got: %s", content)
	}
}

func TestFullFlow(t *testing.T) {
	setupTestStateDir(t)

	sessionID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	// Step 1: handlePostToolUse with a Read tool event.
	readInput := loadFixture[hook.PostToolUsePayload](t, "posttooluse_read.json")
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
	subagentInput := loadFixture[hook.SubagentStopPayload](t, "subagent_stop.json")
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
	stopInput := loadFixture[hook.StopPayload](t, "stop_simple.json")
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

func TestHandleSessionStart_NewSession(t *testing.T) {
	setupTestStateDir(t)
	rotateEnabled = true
	defer func() { rotateEnabled = false }()

	input := loadFixture[hook.SessionStartPayload](t, "sessionstart_resume.json")
	handleSessionStart(input)

	sf := state.LoadState()
	ss, ok := sf.Sessions[input.SessionID]
	if !ok {
		t.Fatalf("session %q not found in state after handleSessionStart", input.SessionID)
	}
	// New session: epoch stays 0, no rotation.
	if ss.Epoch != 0 {
		t.Errorf("Epoch = %d, want 0 for new session", ss.Epoch)
	}
}

func TestHandleSessionStart_ExistingSession_Rotates(t *testing.T) {
	setupTestStateDir(t)
	rotateEnabled = true
	defer func() { rotateEnabled = false }()

	// Pre-seed state with an existing session.
	sf := state.LoadState()
	sf.Sessions["aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"] = &hook.SessionState{
		TranscriptPath: "/some/path.jsonl",
		TurnCount:      5,
		LastLine:       20,
		Epoch:          0,
		SessionSpanID:  "abc123def456",
		Updated:        time.Now(),
	}
	if err := state.SaveState(sf); err != nil {
		t.Fatalf("save state: %v", err)
	}

	input := loadFixture[hook.SessionStartPayload](t, "sessionstart_resume.json")
	handleSessionStart(input)

	sf = state.LoadState()
	ss := sf.Sessions[input.SessionID]
	if ss.Epoch != 1 {
		t.Errorf("Epoch = %d, want 1 after rotation", ss.Epoch)
	}
	if ss.SessionSpanID != "" {
		t.Errorf("SessionSpanID = %q, want empty after rotation", ss.SessionSpanID)
	}
}

func TestHandleSessionStart_RotateDisabled_NoOp(t *testing.T) {
	setupTestStateDir(t)
	rotateEnabled = false

	// Pre-seed state.
	sf := state.LoadState()
	sf.Sessions["aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"] = &hook.SessionState{
		TurnCount:     5,
		Epoch:         0,
		SessionSpanID: "abc123def456",
		Updated:       time.Now(),
	}
	if err := state.SaveState(sf); err != nil {
		t.Fatalf("save state: %v", err)
	}

	input := loadFixture[hook.SessionStartPayload](t, "sessionstart_resume.json")
	handleSessionStart(input)

	sf = state.LoadState()
	ss := sf.Sessions[input.SessionID]
	// Should NOT rotate when flag is off.
	if ss.Epoch != 0 {
		t.Errorf("Epoch = %d, want 0 when rotate disabled", ss.Epoch)
	}
	if ss.SessionSpanID != "abc123def456" {
		t.Errorf("SessionSpanID = %q, want unchanged", ss.SessionSpanID)
	}
}

func TestHandleSessionStart_TraceparentSuppresses(t *testing.T) {
	setupTestStateDir(t)
	rotateEnabled = true
	defer func() { rotateEnabled = false }()
	t.Setenv("TRACEPARENT", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")

	// Pre-seed state.
	sf := state.LoadState()
	sf.Sessions["aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"] = &hook.SessionState{
		TurnCount:     5,
		Epoch:         0,
		SessionSpanID: "abc123def456",
		Updated:       time.Now(),
	}
	if err := state.SaveState(sf); err != nil {
		t.Fatalf("save state: %v", err)
	}

	input := loadFixture[hook.SessionStartPayload](t, "sessionstart_resume.json")
	handleSessionStart(input)

	sf = state.LoadState()
	ss := sf.Sessions[input.SessionID]
	// TRACEPARENT set: should NOT rotate.
	if ss.Epoch != 0 {
		t.Errorf("Epoch = %d, want 0 when TRACEPARENT set", ss.Epoch)
	}
	if ss.SessionSpanID != "abc123def456" {
		t.Errorf("SessionSpanID = %q, want unchanged when TRACEPARENT set", ss.SessionSpanID)
	}
}
