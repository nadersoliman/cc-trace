# Repo Restructure Design

**Goal:** Reorganize cc-trace from flat root layout to canonical Go CLI structure using `cmd/` + `internal/` packages.

**Problem:** All 10 Go files (5 source + 5 test) live at root alongside config, docs, and build artifacts. Hard to navigate; no separation of concerns.

## Target Layout

```
cc-trace/
├── cmd/
│   └── cc-trace/
│       └── main.go              # stdin parsing, event dispatch, main()
├── internal/
│   ├── hook/
│   │   └── types.go             # HookInput, Turn, ToolCall, ToolSpanData, PendingSubagent
│   ├── state/
│   │   ├── state.go             # SessionState, StateFile, load/save/locking
│   │   └── state_test.go
│   ├── transcript/
│   │   ├── parse.go             # parseTranscript, helpers
│   │   └── parse_test.go
│   └── tracer/
│       ├── tracer.go            # OTel init, exportSessionTrace, span creation
│       └── tracer_test.go
├── testdata/fixtures/           # shared fixtures (project root)
├── docs/plans/
├── .github/workflows/
├── .githooks/
├── Makefile
├── go.mod, go.sum
├── README.md, CLAUDE.md, LICENSE
```

## Package Responsibilities

| Package | Files | Purpose |
|---------|-------|---------|
| `cmd/cc-trace` | `main.go` | Entry point, stdin parsing, event dispatch (`handlePostToolUse`, `handleStop`, etc.) |
| `internal/hook` | `types.go` | Shared data structures: `HookInput`, `Turn`, `ToolCall`, `ToolSpanData`, `PendingSubagent` |
| `internal/state` | `state.go` | `SessionState`, `StateFile`, load/save with file locking, `initStatePaths` |
| `internal/transcript` | `parse.go` | `ParseTranscript`, JSONL parsing helpers, `MergeAssistantParts` |
| `internal/tracer` | `tracer.go` | OTel SDK init, `ExportSessionTrace`, deterministic trace IDs, span creation |

## Import Paths

```go
import (
    "cc-trace/internal/hook"
    "cc-trace/internal/state"
    "cc-trace/internal/transcript"
    "cc-trace/internal/tracer"
)
```

## Dependency Graph

```
cmd/cc-trace/main.go
  ├── internal/hook       (types only, no logic)
  ├── internal/state      → depends on hook
  ├── internal/transcript → depends on hook
  └── internal/tracer     → depends on hook, transcript, state
```

No circular dependencies. `internal/hook` is a leaf package with types only.

## Test Fixtures

Fixtures stay at project root in `testdata/fixtures/`. Each internal package's tests reference `../../testdata/fixtures/` (Go test sets cwd to package directory).

## Build Changes

- `go build -o cc-trace ./cmd/cc-trace` replaces `go build -o cc-trace .`
- Install target copies binary as `cc-trace` (unchanged)
- CI: `go test ./...` already recurses into subdirectories

## Visibility Changes

Functions and types currently unexported (lowercase) become exported where needed across package boundaries:
- `parseTranscript` → `transcript.ParseTranscript`
- `exportSessionTrace` → `tracer.ExportSessionTrace`
- `loadState`/`saveState` → `state.LoadState`/`state.SaveState`
- `initTracer`/`initTracerWithExporter` → `tracer.InitTracer`/`tracer.InitTracerWithExporter`

Handler functions (`handlePostToolUse`, `handleStop`, `handleSubagentStop`) stay in `cmd/cc-trace/main.go` as unexported — they're orchestration, not domain logic.
