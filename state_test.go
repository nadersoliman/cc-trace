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
	// Create the state directory so lock/state files can be written.
	stateDir := filepath.Join(dir, ".claude", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("failed to create state dir: %v", err)
	}
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

	// Create the state directory and write corrupt JSON.
	stateDir := filepath.Join(dir, ".claude", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("failed to create state dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "otel_trace_state.json"), []byte("{corrupt"), 0o644); err != nil {
		t.Fatalf("failed to write corrupt state file: %v", err)
	}

	sf := loadState()
	if sf == nil {
		t.Fatal("loadState returned nil on corrupt file")
	}
	if sf.Sessions == nil {
		t.Fatal("Sessions map is nil on corrupt file")
	}
	if len(sf.Sessions) != 0 {
		t.Errorf("expected 0 sessions on corrupt file, got %d", len(sf.Sessions))
	}
}

func TestSaveAndLoad(t *testing.T) {
	setupTestStateDir(t)

	now := time.Now()
	sf := &StateFile{
		Sessions: map[string]*SessionState{
			"test-session-1": {
				TranscriptPath: "/tmp/test.jsonl",
				CWD:            "/tmp",
				LastLine:       42,
				TurnCount:      3,
				Updated:        now,
			},
		},
	}

	if err := saveState(sf); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}

	loaded := loadState()
	if loaded == nil {
		t.Fatal("loadState returned nil")
	}

	ss, ok := loaded.Sessions["test-session-1"]
	if !ok {
		t.Fatal("session 'test-session-1' not found after round-trip")
	}
	if ss.LastLine != 42 {
		t.Errorf("LastLine: got %d, want 42", ss.LastLine)
	}
	if ss.TurnCount != 3 {
		t.Errorf("TurnCount: got %d, want 3", ss.TurnCount)
	}
}

func TestSaveState_PrunesStale(t *testing.T) {
	setupTestStateDir(t)

	now := time.Now()
	sf := &StateFile{
		Sessions: map[string]*SessionState{
			"fresh-session": {
				TranscriptPath: "/tmp/fresh.jsonl",
				CWD:            "/tmp",
				LastLine:       10,
				TurnCount:      1,
				Updated:        now,
			},
			"stale-session": {
				TranscriptPath: "/tmp/stale.jsonl",
				CWD:            "/tmp",
				LastLine:       5,
				TurnCount:      2,
				Updated:        now.Add(-25 * time.Hour),
			},
		},
	}

	if err := saveState(sf); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}

	loaded := loadState()
	if loaded == nil {
		t.Fatal("loadState returned nil")
	}

	if _, ok := loaded.Sessions["fresh-session"]; !ok {
		t.Error("fresh session was pruned but should have been kept")
	}
	if _, ok := loaded.Sessions["stale-session"]; ok {
		t.Error("stale session was kept but should have been pruned")
	}
}

func TestSaveAndLoad_WithToolSpans(t *testing.T) {
	setupTestStateDir(t)

	now := time.Now()
	sf := &StateFile{
		Sessions: map[string]*SessionState{
			"tool-session": {
				TranscriptPath: "/tmp/tools.jsonl",
				CWD:            "/tmp",
				LastLine:       10,
				TurnCount:      1,
				ToolSpans: []ToolSpanData{
					{
						ToolName:  "Read",
						ToolUseID: "toolu_test_001",
						ToolInput: map[string]interface{}{
							"file_path": "/tmp/main.go",
						},
						Timestamp: now,
					},
				},
				Updated: now,
			},
		},
	}

	if err := saveState(sf); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}

	loaded := loadState()
	if loaded == nil {
		t.Fatal("loadState returned nil")
	}

	ss, ok := loaded.Sessions["tool-session"]
	if !ok {
		t.Fatal("session 'tool-session' not found after round-trip")
	}
	if len(ss.ToolSpans) != 1 {
		t.Fatalf("expected 1 tool span, got %d", len(ss.ToolSpans))
	}
	if ss.ToolSpans[0].ToolName != "Read" {
		t.Errorf("ToolName: got %q, want %q", ss.ToolSpans[0].ToolName, "Read")
	}
	if ss.ToolSpans[0].ToolUseID != "toolu_test_001" {
		t.Errorf("ToolUseID: got %q, want %q", ss.ToolSpans[0].ToolUseID, "toolu_test_001")
	}
}

func TestLocking(t *testing.T) {
	setupTestStateDir(t)

	// First acquire should succeed.
	if !acquireLock() {
		t.Fatal("first acquireLock should return true")
	}

	// Second acquire while held should fail.
	if acquireLock() {
		t.Fatal("second acquireLock should return false (lock held)")
	}

	// Release and re-acquire should succeed.
	releaseLock()

	if !acquireLock() {
		t.Fatal("acquireLock after releaseLock should return true")
	}
	releaseLock()
}

func TestStaleLockRemoval(t *testing.T) {
	dir := setupTestStateDir(t)

	// Create the state directory and a stale lock file.
	stateDir := filepath.Join(dir, ".claude", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("failed to create state dir: %v", err)
	}

	// Create lock file.
	lockFile := filepath.Join(stateDir, "otel_trace_state.lock")
	f, err := os.Create(lockFile)
	if err != nil {
		t.Fatalf("failed to create lock file: %v", err)
	}
	f.Close()

	// Set modification time to 10 minutes ago (beyond staleLockAge of 5 minutes).
	staleTime := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(lockFile, staleTime, staleTime); err != nil {
		t.Fatalf("failed to set lock file mtime: %v", err)
	}

	// acquireLock should succeed because the stale lock is removed.
	if !acquireLock() {
		t.Fatal("acquireLock should return true after removing stale lock")
	}
	releaseLock()
}

func TestLoadState_FromFixture(t *testing.T) {
	fp := fixturePath(t, "state_with_tools.json")

	data, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	var sf StateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatalf("failed to unmarshal fixture: %v", err)
	}

	sessionID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	ss, ok := sf.Sessions[sessionID]
	if !ok {
		t.Fatalf("session %q not found in fixture", sessionID)
	}

	if len(ss.ToolSpans) != 1 {
		t.Fatalf("expected 1 tool span, got %d", len(ss.ToolSpans))
	}
	if ss.ToolSpans[0].ToolName != "Read" {
		t.Errorf("ToolName: got %q, want %q", ss.ToolSpans[0].ToolName, "Read")
	}
}
