# SessionStart Trace Rotation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Move trace rotation from `handleStop` to a new `handleSessionStart`, triggered by the `SessionStart` hook event.

**Architecture:** Add `SessionStartPayload` type, wire it into the two-phase dispatch in `main()`, implement `handleSessionStart` that increments epoch and clears SessionSpanID for existing sessions when `CC_TRACE_ROTATE=true` and no `TRACEPARENT`. Remove rotation logic from `handleStop`.

**Tech Stack:** Go, OTel SDK, existing state/hook packages

---

### Task 1: Add SessionStartPayload and fixture

**Files:**
- Modify: `internal/hook/types.go:14` (add after `PostToolUsePayload`)
- Create: `testdata/fixtures/sessionstart_resume.json`

**Step 1: Add SessionStartPayload to types.go**

Add after `PostToolUsePayload` (after line 21):

```go
// SessionStartPayload is the schema for SessionStart hook events.
type SessionStartPayload struct {
	HookBase
	Source string `json:"source"` // "startup", "resume", "clear", "compact"
	Model  string `json:"model"`
}
```

**Step 2: Create test fixture**

Create `testdata/fixtures/sessionstart_resume.json`:

```json
{
  "session_id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
  "transcript_path": "/home/testuser/.claude/projects/test-project/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.jsonl",
  "cwd": "/home/testuser/projects/cc-trace",
  "hook_event_name": "SessionStart",
  "permission_mode": "default",
  "source": "resume",
  "model": "claude-sonnet-4-6"
}
```

**Step 3: Verify it compiles**

Run: `cd /Users/nadersoliman/projects/cc-trace && go build ./...`
Expected: success, no errors

**Step 4: Commit**

```bash
git add internal/hook/types.go testdata/fixtures/sessionstart_resume.json
git commit -m "feat: add SessionStartPayload type and fixture" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 2: Write failing tests for handleSessionStart

**Files:**
- Modify: `cmd/cc-trace/main_test.go`

**Step 1: Write tests for SessionStart rotation**

Add these tests at the end of `main_test.go`:

```go
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
		LastLine:        20,
		Epoch:          0,
		SessionSpanID:  "abc123def456",
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
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/nadersoliman/projects/cc-trace && go test ./cmd/cc-trace/ -run TestHandleSessionStart -v`
Expected: FAIL — `handleSessionStart` undefined

**Step 3: Commit failing tests**

```bash
git add cmd/cc-trace/main_test.go
git commit -m "test: add failing tests for handleSessionStart" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Implement handleSessionStart and wire dispatch

**Files:**
- Modify: `cmd/cc-trace/main.go:68` (dispatch switch) and add handler function

**Step 1: Add SessionStart dispatch case**

In the switch block at line 68 of `main.go`, add before the `"PostToolUse"` case:

```go
	case "SessionStart":
		var input hook.SessionStartPayload
		if err := json.Unmarshal(data, &input); err != nil {
			logging.Log("ERROR", fmt.Sprintf("Failed to parse SessionStart: %v", err))
			os.Exit(0)
		}
		handleSessionStart(input)
```

**Step 2: Add handleSessionStart function**

Add after `handleSubagentStop` (end of file):

```go
func handleSessionStart(input hook.SessionStartPayload) {
	start := time.Now()

	if !rotateEnabled {
		logging.Debug("Rotation disabled, skipping SessionStart")
		return
	}
	if os.Getenv("TRACEPARENT") != "" {
		logging.Debug("TRACEPARENT set, skipping SessionStart rotation")
		return
	}
	if input.SessionID == "" {
		logging.Debug("No session_id in SessionStart, skipping")
		return
	}

	lockStart := time.Now()
	if !state.AcquireLock() {
		logging.Debug("Lock held, skipping SessionStart")
		return
	}
	defer state.ReleaseLock()
	lockDur := time.Since(lockStart)

	loadStart := time.Now()
	sf := state.LoadState()
	loadDur := time.Since(loadStart)

	ss, ok := sf.Sessions[input.SessionID]
	if !ok {
		// Brand new session — initialize at epoch 0, no rotation needed.
		sf.Sessions[input.SessionID] = &hook.SessionState{
			TranscriptPath: input.TranscriptPath,
			CWD:            input.CWD,
		}
	} else {
		// Existing session — rotate trace.
		ss.Epoch++
		ss.SessionSpanID = ""
		ss.Updated = time.Now()
		logging.Debug(fmt.Sprintf("Rotated trace for session %s to epoch %d", truncate(input.SessionID, 12), ss.Epoch))
	}

	saveStart := time.Now()
	if err := state.SaveState(sf); err != nil {
		logging.Log("ERROR", fmt.Sprintf("Failed to save state: %v", err))
	}
	saveDur := time.Since(saveStart)

	sid := input.SessionID
	if len(sid) > 8 {
		sid = sid[:8]
	}
	logging.Timing(fmt.Sprintf("total=%dms SessionStart session=%s source=%s lock=%dms load=%dms save=%dms",
		time.Since(start).Milliseconds(), sid, input.Source,
		lockDur.Milliseconds(), loadDur.Milliseconds(), saveDur.Milliseconds()))
}
```

Note: `truncate` is in `internal/tracer/tracer.go` — since it's unexported, duplicate a local `truncateID` or just use the `sid[:8]` pattern already used in other handlers. Simplest: just use `sid` (already truncated above).

Actually, looking at the existing handlers, they all truncate `sid` to 8 chars at the end for logging. The `truncate` reference in the design's pseudocode was for the Debug log. Replace `truncate(input.SessionID, 12)` with `sid` in the Debug line (it's already truncated by that point in the function). Or just use `input.SessionID[:12]` inline. Simplest: use `sid` which is already 8 chars.

**Step 3: Run tests to verify they pass**

Run: `cd /Users/nadersoliman/projects/cc-trace && go test ./cmd/cc-trace/ -run TestHandleSessionStart -v`
Expected: all 4 tests PASS

**Step 4: Run full test suite**

Run: `cd /Users/nadersoliman/projects/cc-trace && go test ./... -v`
Expected: all tests PASS

**Step 5: Commit**

```bash
git add cmd/cc-trace/main.go
git commit -m "feat: add handleSessionStart with trace rotation logic" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 4: Remove rotation from handleStop

**Files:**
- Modify: `cmd/cc-trace/main.go:351-356`

**Step 1: Delete the rotation block in handleStop**

Remove these lines (currently 351-356):

```go
	// Rotation only applies in standalone mode (no TRACEPARENT).
	// When an external trace provides the trace ID, epoch rotation is meaningless.
	if rotateEnabled && os.Getenv("TRACEPARENT") == "" {
		ss.Epoch++
		ss.SessionSpanID = ""
	}
```

**Step 2: Run full test suite**

Run: `cd /Users/nadersoliman/projects/cc-trace && go test ./... -v`
Expected: all tests PASS

**Step 3: Commit**

```bash
git add cmd/cc-trace/main.go
git commit -m "fix: remove rotation from handleStop (now in handleSessionStart)" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 5: Update TestExportSessionTrace_Rotation to match new flow

**Files:**
- Modify: `internal/tracer/tracer_test.go:510-583`

The existing `TestExportSessionTrace_Rotation` test simulates rotation by manually doing `ss.Epoch++` and `ss.SessionSpanID = ""` between exports — this was mimicking what `handleStop` used to do. Update the comment to reflect that this is now what `handleSessionStart` does:

**Step 1: Update the comment**

In `tracer_test.go`, around line 542-543, change the comment from:

```go
	// Simulate handleStop: increment epoch, clear span ID
```

to:

```go
	// Simulate handleSessionStart: increment epoch, clear span ID
```

**Step 2: Run tests**

Run: `cd /Users/nadersoliman/projects/cc-trace && go test ./internal/tracer/ -run TestExportSessionTrace_Rotation -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/tracer/tracer_test.go
git commit -m "test: update rotation test comment to reflect SessionStart flow" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 6: Update documentation

**Files:**
- Modify: `CLAUDE.md`
- Modify: `README.md`

**Step 1: Update CLAUDE.md architecture section**

In `CLAUDE.md`, update the architecture description. Change:

> Short-lived CLI invoked by Claude Code on **PostToolUse**, **PostToolUseFailure**, **SubagentStop**, and **Stop** hook events via stdin JSON.

to:

> Short-lived CLI invoked by Claude Code on **SessionStart**, **PostToolUse**, **PostToolUseFailure**, **SubagentStop**, and **Stop** hook events via stdin JSON.

Add a bullet for SessionStart in the behavior list:

> - **SessionStart** (< 10ms, no network): Rotates trace ID when `CC_TRACE_ROTATE=true` (increments epoch, clears SessionSpanID)

Update `CC_TRACE_ROTATE` description in the env var table from:

> Rotate trace ID per resume. Each Stop gets its own self-contained trace, preventing long-lived sessions from outliving backend retention. Ignored when `TRACEPARENT` is set.

to:

> Rotate trace ID per session segment. Each SessionStart (startup/resume/clear/compact) on an existing session creates a new trace, preventing long-lived sessions from outliving backend retention. Ignored when `TRACEPARENT` is set.

**Step 2: Update README.md**

In `README.md`, update the hooks configuration to include SessionStart:

```jsonc
{
  "hooks": {
    "SessionStart": [{ "hooks": [{ "type": "command", "command": "~/.claude/hooks/cc-trace" }] }],
    "PostToolUse": [{ "hooks": [{ "type": "command", "command": "~/.claude/hooks/cc-trace" }] }],
    "PostToolUseFailure": [{ "hooks": [{ "type": "command", "command": "~/.claude/hooks/cc-trace" }] }],
    "SubagentStop": [{ "hooks": [{ "type": "command", "command": "~/.claude/hooks/cc-trace" }] }],
    "Stop": [{ "hooks": [{ "type": "command", "command": "~/.claude/hooks/cc-trace" }] }]
  }
}
```

Update `CC_TRACE_ROTATE` description in the env var table to match CLAUDE.md.

Update the "How It Works" section:

> Short-lived Go CLI invoked by Claude Code via stdin JSON. **SessionStart** rotates the trace when `CC_TRACE_ROTATE` is enabled. **PostToolUse** / **PostToolUseFailure** and **SubagentStop** record data locally with zero network calls (< 10ms). On **Stop**, the hook parses the session transcript, builds the span tree, and exports via OTLP/HTTP.

**Step 3: Commit**

```bash
git add CLAUDE.md README.md
git commit -m "docs: add SessionStart to architecture and setup instructions" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 7: Run full test suite and verify

**Step 1: Run all tests**

Run: `cd /Users/nadersoliman/projects/cc-trace && go test ./... -v -count=1`
Expected: all tests PASS

**Step 2: Build binary**

Run: `cd /Users/nadersoliman/projects/cc-trace && go build -o /dev/null ./cmd/cc-trace/`
Expected: success

**Step 3: Run vet and check for issues**

Run: `cd /Users/nadersoliman/projects/cc-trace && go vet ./...`
Expected: no issues
