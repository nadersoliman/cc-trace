# Naming Consistency Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Rename all legacy `otel_trace_hook` / `CC_OTEL_TRACE_*` references to `cc_trace` / `CC_TRACE_*` for consistency before public launch.

**Architecture:** Mechanical find-and-replace across Go source, tests, and documentation. No migration — clean break on state files.

**Tech Stack:** Go, no new dependencies

---

### Task 1: Update state file and lock file names

**Files:**
- Modify: `internal/state/state_test.go:61,236`
- Modify: `internal/state/state.go:22-23`

**Step 1: Update test references to new file names**

In `internal/state/state_test.go`, change two lines:

Line 61 — change `"otel_trace_state.json"` to `"cc_trace_state.json"`:
```go
	if err := os.WriteFile(filepath.Join(stateDir, "cc_trace_state.json"), []byte("{corrupt"), 0o644); err != nil {
```

Line 236 — change `"otel_trace_state.lock"` to `"cc_trace_state.lock"`:
```go
	lockFile := filepath.Join(stateDir, "cc_trace_state.lock")
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/state/ -v`
Expected: `TestLoadState_Corrupt` and `TestStaleLockRemoval` FAIL (writing to new file names but code reads old ones)

**Step 3: Update state.go file paths**

In `internal/state/state.go`, change lines 22-23:

```go
	stateFilePath = filepath.Join(stateDir, "cc_trace_state.json")
	lockFilePath = filepath.Join(stateDir, "cc_trace_state.lock")
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/state/ -v`
Expected: All tests PASS

**Step 5: Commit**

```bash
git add internal/state/state.go internal/state/state_test.go
git commit --author="Claude Opus 4.6 <noreply@anthropic.com>" -m "refactor: rename state/lock files from otel_trace_state to cc_trace_state"
```

---

### Task 2: Rename environment variables and log file

**Files:**
- Modify: `cmd/cc-trace/main.go:30-33`

**Step 1: Update main.go**

Change lines 30-33:

```go
	logFilePath := filepath.Join(homeDir, ".claude", "state", "cc_trace.log")
	debugEnabled := strings.EqualFold(os.Getenv("CC_TRACE_DEBUG"), "true")
	dumpEnabled = strings.EqualFold(os.Getenv("CC_TRACE_DUMP"), "true")
	timingEnabled := strings.EqualFold(os.Getenv("CC_TRACE_TIMING"), "true")
```

**Step 2: Build and run tests**

Run: `go build ./cmd/cc-trace/ && go test ./... -v`
Expected: Build succeeds, all tests pass

**Step 3: Commit**

```bash
git add cmd/cc-trace/main.go
git commit --author="Claude Opus 4.6 <noreply@anthropic.com>" -m "refactor: rename env vars CC_OTEL_TRACE_* to CC_TRACE_* and log file to cc_trace.log"
```

---

### Task 3: Clean up .gitignore

**Files:**
- Modify: `.gitignore`

**Step 1: Remove stale `otel_trace_hook` line**

Change `.gitignore` from:
```
# Compiled binary
cc-trace
otel_trace_hook

# OS
.DS_Store
```

To:
```
# Compiled binary
cc-trace

# OS
.DS_Store
```

**Step 2: Commit**

```bash
git add .gitignore
git commit --author="Claude Opus 4.6 <noreply@anthropic.com>" -m "chore: remove stale otel_trace_hook from .gitignore"
```

---

### Task 4: Update CLAUDE.md and README.md

**Files:**
- Modify: `CLAUDE.md:10,17,61-63`
- Modify: `README.md:65-66`

**Step 1: Update CLAUDE.md**

Line 10 — fix make install comment:
```
make install    # builds and copies binary to ~/.claude/hooks/cc-trace
```

Line 17 — fix state file path:
```
- **PostToolUse / PostToolUseFailure** (< 10ms, no network): Records tool data to `~/.claude/state/cc_trace_state.json`
```

Lines 61-63 — rename env vars and log file paths:
```
| `CC_TRACE_DEBUG` | `false` | Debug logging to `~/.claude/state/cc_trace.log` |
| `CC_TRACE_TIMING` | `false` | Phase-level timing logs to `~/.claude/state/cc_trace.log` (format: `total=Nms EventName session=... phase=Nms ...`) |
| `CC_TRACE_DUMP` | `false` | Dump raw hook payloads and transcripts to `/tmp/cc-trace/dumps/` for investigation |
```

**Step 2: Update README.md**

Lines 65-66 — rename env vars and log file paths:
```
| `CC_TRACE_DEBUG` | `false` | Debug log to `~/.claude/state/cc_trace.log` |
| `CC_TRACE_TIMING` | `false` | Phase-level timing logs to `~/.claude/state/cc_trace.log` |
```

**Step 3: Commit**

```bash
git add CLAUDE.md README.md
git commit --author="Claude Opus 4.6 <noreply@anthropic.com>" -m "docs: rename CC_OTEL_TRACE_* to CC_TRACE_* in CLAUDE.md and README.md"
```

---

### Task 5: Update historical plan docs

**Files:**
- Modify: `docs/plans/2026-03-02-repo-restructure-design.md`
- Modify: `docs/plans/2026-03-02-repo-restructure-implementation.md`
- Modify: `docs/plans/2026-03-01-testing-implementation.md`

**Step 1: Find and replace all occurrences**

In all three files, replace:
- `otel_trace_hook` → `cc-trace` (binary name)
- `otel_trace_state` → `cc_trace_state` (state/lock files)

Use `grep` to find exact lines, then `Edit` to replace.

**Step 2: Commit**

```bash
git add docs/plans/
git commit --author="Claude Opus 4.6 <noreply@anthropic.com>" -m "docs: update historical plan docs for cc-trace naming"
```

---

### Task 6: Final verification

**Step 1: Run full test suite**

Run: `go test ./... -v`
Expected: All tests PASS

**Step 2: Build binary**

Run: `go build ./cmd/cc-trace/`
Expected: Build succeeds

**Step 3: Verify no old naming remains**

Run: `grep -r "otel_trace" --include="*.go" --include="*.md" .`
Expected: No matches (or only in this plan file itself)

**Step 4: Install**

Run: `make install`
