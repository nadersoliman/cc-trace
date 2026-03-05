# Repo Restructure Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Restructure cc-trace from flat root layout to canonical Go CLI layout with `cmd/` + `internal/` packages.

**Architecture:** Create 4 internal packages (`hook`, `logging`, `transcript`, `state`, `tracer`) and move entry point to `cmd/cc-trace/main.go`. All types live in `internal/hook/` as the shared types package. Each task creates new packages alongside old code (so compilation never breaks mid-refactor), then a final task does the switchover.

**Tech Stack:** Go modules, `internal/` package visibility

---

### Task 1: Create leaf packages (hook types + logging)

**Files:**
- Create: `internal/hook/types.go`
- Create: `internal/logging/logging.go`

**Step 1: Create `internal/hook/types.go`**

Copy all type definitions from root `types.go` into `package hook`. Export all types (they're already uppercase). Add the `time` import.

Types to include: `HookInput`, `ToolSpanData`, `PendingSubagent`, `SessionState`, `StateFile`, `Turn`, `ToolCall`.

**Step 2: Create `internal/logging/logging.go`**

Extract `logMsg` and `debugLog` from root `main.go` into `package logging`. Design:

```go
package logging

import (...)

var (
    filePath     string
    debugEnabled bool
)

func Init(logFilePath string, debug bool) {
    filePath = logFilePath
    debugEnabled = debug
}

func Log(level, message string) { /* same as current logMsg */ }
func Debug(message string) { /* same as current debugLog */ }
```

**Step 3: Verify**

Run: `go build ./internal/hook/... ./internal/logging/...`
Expected: success (old root code unchanged, no conflicts)

**Step 4: Commit**

```bash
git add internal/
git commit -m "Add internal/hook and internal/logging packages" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 2: Create internal/transcript package

**Files:**
- Create: `internal/transcript/parse.go`
- Create: `internal/transcript/parse_test.go`

**Step 1: Create `internal/transcript/parse.go`**

Copy all functions from root `transcript.go` into `package transcript`. Key changes:

- `import "cc-trace/internal/hook"` for `hook.Turn` and `hook.ToolCall`
- Export `ParseTranscript` (the main entry point). All helper functions (`msgRole`, `extractTimestamp`, `getToolCalls`, etc.) stay unexported — tests are in the same package.
- `MergeAssistantParts` can stay unexported (only used within `ParseTranscript`, tested from same package).
- Replace all `Turn{...}` with `hook.Turn{...}` and `ToolCall{...}` with `hook.ToolCall{...}`.
- Function signature: `func ParseTranscript(transcriptPath string, startLine int) ([]hook.Turn, int, error)`

**Step 2: Create `internal/transcript/parse_test.go`**

Copy root `transcript_test.go` into `package transcript`. Key changes:

- `package transcript` (not `package transcript_test` — needs access to unexported helpers)
- Update fixture path helper: `filepath.Join("..", "..", "testdata", "fixtures", name)`
- Replace `Turn{...}` with `hook.Turn{...}`, `ToolCall` with `hook.ToolCall`
- Replace `parseTranscript(...)` with `ParseTranscript(...)`

**Step 3: Verify**

Run: `go test -v ./internal/transcript/...`
Expected: All transcript tests pass

**Step 4: Commit**

```bash
git add internal/transcript/
git commit -m "Add internal/transcript package with tests" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Create internal/state package

**Files:**
- Create: `internal/state/state.go`
- Create: `internal/state/state_test.go`

**Step 1: Create `internal/state/state.go`**

Copy all functions from root `state.go` into `package state`. Key changes:

- `import "cc-trace/internal/hook"` for `hook.SessionState`, `hook.StateFile`
- `import "cc-trace/internal/logging"` — replace `debugLog(...)` with `logging.Debug(...)`, `logMsg(...)` with `logging.Log(...)`
- Export: `Init` (was `initStatePaths`), `AcquireLock`, `ReleaseLock`, `LoadState` (was `loadState`), `SaveState` (was `saveState`)
- Package vars `stateFilePath`, `lockFilePath` stay unexported
- Function signatures use `*hook.StateFile` instead of `*StateFile`

```go
func Init(homeDir string) { ... }
func AcquireLock() bool { ... }
func ReleaseLock() { ... }
func LoadState() *hook.StateFile { ... }
func SaveState(sf *hook.StateFile) error { ... }
```

**Step 2: Create `internal/state/state_test.go`**

Copy root `state_test.go` into `package state`. Key changes:

- `package state` (same package for access to unexported vars)
- `import "cc-trace/internal/hook"` for types
- Replace `initStatePaths(...)` with `Init(...)`
- Replace `loadState()` with `LoadState()`, `saveState(...)` with `SaveState(...)`
- Replace `acquireLock()` with `AcquireLock()`, `releaseLock()` with `ReleaseLock()`
- Replace type names: `SessionState` → `hook.SessionState`, `StateFile` → `hook.StateFile`, `ToolSpanData` → `hook.ToolSpanData`
- Update fixture path: `filepath.Join("..", "..", "testdata", "fixtures", name)`
- The `setupTestStateDir(t)` helper calls `Init(t.TempDir())` and creates `.claude/state/` subdir

**Step 3: Verify**

Run: `go test -v ./internal/state/...`
Expected: All state tests pass

**Step 4: Commit**

```bash
git add internal/state/
git commit -m "Add internal/state package with tests" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 4: Create internal/tracer package

**Files:**
- Create: `internal/tracer/tracer.go`
- Create: `internal/tracer/tracer_test.go`

**Step 1: Create `internal/tracer/tracer.go`**

Copy all functions from root `tracer.go` into `package tracer`. Key changes:

- Import `cc-trace/internal/hook` and `cc-trace/internal/logging`
- Replace `debugLog(...)` → `logging.Debug(...)`, `logMsg(...)` → `logging.Log(...)`
- Export: `InitTracer`, `InitTracerWithExporter`, `ExportSessionTrace`
- Keep unexported: `traceIDFromSession`, `matchSubagent`, `emitSubagentSpans`, `parseTraceparent`, `contextWithTraceID`, `truncate`
- Function signatures use `hook.Turn`, `hook.ToolSpanData`, `hook.PendingSubagent`, `hook.SessionState`, `hook.ToolCall`

```go
func InitTracer() (func(), error) { ... }
func InitTracerWithExporter(exporter sdktrace.SpanExporter) (func(), error) { ... }
func ExportSessionTrace(sessionID string, turns []hook.Turn, toolSpanData []hook.ToolSpanData, pendingSubagents []hook.PendingSubagent, ss *hook.SessionState) { ... }
```

**Step 2: Create `internal/tracer/tracer_test.go`**

Copy root `tracer_test.go` into `package tracer`. Key changes:

- `package tracer` (same package for access to unexported helpers)
- Import `cc-trace/internal/hook` for types
- Replace `initTracerWithExporter(...)` → `InitTracerWithExporter(...)`
- Replace `exportSessionTrace(...)` → `ExportSessionTrace(...)`
- Replace `traceIDFromSession(...)` — stays as-is (unexported, same package)
- Replace type names with `hook.` prefix
- Update fixture path: `filepath.Join("..", "..", "testdata", "fixtures", name)`

**Step 3: Verify**

Run: `go test -v ./internal/tracer/...`
Expected: All tracer tests pass

**Step 4: Commit**

```bash
git add internal/tracer/
git commit -m "Add internal/tracer package with tests" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 5: Create cmd/cc-trace/main.go and delete old root files

This is the switchover task. After this, the old root-level Go files are gone.

**Files:**
- Create: `cmd/cc-trace/main.go`
- Create: `cmd/cc-trace/main_test.go`
- Delete: `main.go`, `types.go`, `state.go`, `transcript.go`, `tracer.go`
- Delete: `main_test.go`, `state_test.go`, `transcript_test.go`, `tracer_test.go`, `testhelpers_test.go`

**Step 1: Create `cmd/cc-trace/main.go`**

Write `package main` that imports all internal packages and wires them together:

```go
package main

import (
    "cc-trace/internal/hook"
    "cc-trace/internal/logging"
    "cc-trace/internal/state"
    "cc-trace/internal/tracer"
    "cc-trace/internal/transcript"
    ...
)
```

- Move `init()` logic into top of `main()`: call `logging.Init(...)`, `state.Init(...)`.
- Keep global vars: `homeDir`, `dumpEnabled`, `dumpDir` (only used in this file).
- Keep functions: `main()`, `dumpPayload()`, `dumpTranscript()`, `handlePostToolUse()`, `handleStop()`, `handleSubagentStop()`.
- Update handler functions to call exported package functions:
  - `acquireLock()` → `state.AcquireLock()`
  - `releaseLock()` → `state.ReleaseLock()`
  - `loadState()` → `state.LoadState()`
  - `saveState(sf)` → `state.SaveState(sf)`
  - `parseTranscript(...)` → `transcript.ParseTranscript(...)`
  - `initTracer()` → `tracer.InitTracer()`
  - `exportSessionTrace(...)` → `tracer.ExportSessionTrace(...)`
  - `debugLog(...)` → `logging.Debug(...)`
  - `logMsg(...)` → `logging.Log(...)`
- All type references use `hook.` prefix: `hook.HookInput`, `hook.ToolSpanData`, etc.

**Step 2: Create `cmd/cc-trace/main_test.go`**

Copy root `main_test.go` into `package main` under `cmd/cc-trace/`. Key changes:

- Import internal packages for types
- Update function calls to use package-qualified names
- Fixture path: `filepath.Join("..", "..", "testdata", "fixtures", name)`
- `setupTestStateDir(t)` calls `state.Init(t.TempDir())` and creates the `.claude/state/` dir
- `handlePostToolUse`, `handleStop`, `handleSubagentStop` stay as unexported calls (same package)

**Step 3: Delete old root-level Go files**

```bash
rm main.go types.go state.go transcript.go tracer.go
rm main_test.go state_test.go transcript_test.go tracer_test.go testhelpers_test.go
```

**Step 4: Verify**

Run: `go build ./cmd/cc-trace/` and `go test ./...`
Expected: Build succeeds, all tests pass across all packages

**Step 5: Commit**

```bash
git add -A
git commit -m "Move entry point to cmd/cc-trace, delete root Go files" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 6: Update Makefile, CLAUDE.md, and verify

**Files:**
- Modify: `Makefile`
- Modify: `CLAUDE.md`

**Step 1: Update Makefile**

Change build command:

```makefile
BINARY_NAME = cc-trace
INSTALL_DIR = $(HOME)/.claude/hooks

.PHONY: build install clean test test-race test-cover fmt setup-hooks

build:
	go build -o $(BINARY_NAME) ./cmd/cc-trace

install: build
	mkdir -p $(INSTALL_DIR)
	cp $(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
	chmod +x $(INSTALL_DIR)/$(BINARY_NAME)

clean:
	rm -f $(BINARY_NAME)

test:
	go test -v -count=1 ./...

test-race:
	go test -race -v -count=1 ./...

test-cover:
	go test -cover ./...

fmt:
	gofmt -s -w .

setup-hooks:
	git config core.hooksPath .githooks
```

**Step 2: Update CLAUDE.md files table**

Update the Files table to reflect new paths:

```markdown
## Files

| Path | Purpose |
|------|---------|
| `cmd/cc-trace/main.go` | Entry point, stdin parsing, event dispatch |
| `internal/hook/types.go` | Data structures (HookInput, Turn, ToolCall, SessionState) |
| `internal/logging/logging.go` | Debug and error logging to file |
| `internal/state/state.go` | State file load/save with file locking |
| `internal/transcript/parse.go` | JSONL transcript parsing into turns |
| `internal/tracer/tracer.go` | OTel SDK init, span creation, deterministic trace IDs |
```

**Step 3: Verify end-to-end**

Run:
```bash
make build      # Should produce cc-trace binary
make test       # All tests pass
make test-race  # Race-clean
make test-cover # Coverage report
gofmt -s -l .   # No unformatted files
```

**Step 4: Commit**

```bash
git add Makefile CLAUDE.md
git commit -m "Update Makefile and CLAUDE.md for new repo structure" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```
