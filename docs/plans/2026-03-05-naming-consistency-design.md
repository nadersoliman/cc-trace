# Naming Consistency Design

Rename all legacy `otel_trace_hook` / `CC_OTEL_TRACE_*` references to `cc-trace` / `CC_TRACE_*` for consistency before public launch.

## Rename Mapping

| Old | New |
|-----|-----|
| `CC_OTEL_TRACE_DEBUG` | `CC_TRACE_DEBUG` |
| `CC_OTEL_TRACE_TIMING` | `CC_TRACE_TIMING` |
| `CC_OTEL_TRACE_DUMP` | `CC_TRACE_DUMP` |
| `otel_trace_hook.log` | `cc_trace.log` |
| `otel_trace_state.json` | `cc_trace_state.json` |
| `otel_trace_state.lock` | `cc_trace_state.lock` |
| `otel_trace_hook` (.gitignore) | remove (already have `cc-trace`) |
| `~/.claude/hooks/otel_trace_hook` (CLAUDE.md) | `~/.claude/hooks/cc-trace` |

## Files Changed

### Live code

| File | Changes |
|------|---------|
| `cmd/cc-trace/main.go` | 3 env var names, 1 log file path |
| `internal/state/state.go` | state file + lock file paths |
| `internal/state/state_test.go` | references to old file names |
| `.gitignore` | remove `otel_trace_hook` line |

### Documentation

| File | Changes |
|------|---------|
| `CLAUDE.md` | env var table, file paths, make install line |
| `README.md` | env var table |
| `docs/plans/2026-03-02-repo-restructure-design.md` | old binary name references |
| `docs/plans/2026-03-02-repo-restructure-implementation.md` | old binary name references |
| `docs/plans/2026-03-01-testing-implementation.md` | old file name references |

## Migration

None. Clean break — old state file is abandoned.
