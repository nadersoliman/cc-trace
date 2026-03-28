# AcquireLock MkdirAll Fix Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix the bootstrap deadlock where `AcquireLock` silently fails when `~/.claude/state/` does not exist, causing all hook events to be dropped on fresh installs.

**Architecture:** Add `os.MkdirAll` for the parent directory at the top of `AcquireLock()`. This ensures the state directory exists before attempting to create the lock file, breaking the deadlock where the directory can only be created by `SaveState` which is only reached after `AcquireLock` succeeds.

**Tech Stack:** Go, `os` stdlib

---

### Task 1: Write failing test for missing directory

**Files:**
- Modify: `internal/state/state_test.go`

**Step 1: Write the test**

Add at the end of `internal/state/state_test.go`:

```go
func TestAcquireLock_MissingDirectory(t *testing.T) {
	// Init with a temp dir but do NOT create the .claude/state/ subdirectory.
	dir := t.TempDir()
	Init(dir)
	logging.Init(filepath.Join(dir, "test.log"), false)

	// AcquireLock should succeed even when the state directory does not exist.
	if !AcquireLock() {
		t.Fatal("AcquireLock should succeed when state directory is missing (should create it)")
	}
	ReleaseLock()

	// Verify the directory was created.
	stateDir := filepath.Join(dir, ".claude", "state")
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		t.Fatal("state directory should have been created by AcquireLock")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/nadersoliman/projects/cc-trace && go test ./internal/state/ -run TestAcquireLock_MissingDirectory -v`
Expected: FAIL — `AcquireLock should succeed when state directory is missing`

**Step 3: Commit**

```bash
git add internal/state/state_test.go
git commit -m "test: add failing test for AcquireLock with missing directory" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 2: Add os.MkdirAll to AcquireLock

**Files:**
- Modify: `internal/state/state.go:32`

**Step 1: Add directory creation**

In `internal/state/state.go`, at the top of the `AcquireLock` function (line 32), add one line before the stale lock check:

```go
func AcquireLock() bool {
	_ = os.MkdirAll(filepath.Dir(lockFilePath), 0o755)
	if info, err := os.Stat(lockFilePath); err == nil {
```

This adds `_ = os.MkdirAll(filepath.Dir(lockFilePath), 0o755)` as the first line of the function body. `filepath.Dir(lockFilePath)` resolves to `~/.claude/state/` which is the same directory that `SaveState` creates. `MkdirAll` is a no-op if the directory already exists.

**Step 2: Run the failing test**

Run: `cd /Users/nadersoliman/projects/cc-trace && go test ./internal/state/ -run TestAcquireLock_MissingDirectory -v`
Expected: PASS

**Step 3: Run full test suite**

Run: `cd /Users/nadersoliman/projects/cc-trace && go test ./... -v`
Expected: ALL tests PASS

**Step 4: Commit**

```bash
git add internal/state/state.go
git commit -m "fix: ensure state directory exists before acquiring lock" --author="Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Run full verification

**Step 1: Run all tests**

Run: `cd /Users/nadersoliman/projects/cc-trace && go test ./... -v -count=1`
Expected: all tests PASS

**Step 2: Build binary**

Run: `cd /Users/nadersoliman/projects/cc-trace && go build -o /dev/null ./cmd/cc-trace/`
Expected: success

**Step 3: Run vet**

Run: `cd /Users/nadersoliman/projects/cc-trace && go vet ./...`
Expected: no issues
