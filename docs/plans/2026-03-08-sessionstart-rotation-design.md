# SessionStart Trace Rotation

Fixes #22: CC_TRACE_ROTATE rotates on every Stop instead of on resume.

## Problem

The trace rotation implementation increments the epoch after every Stop event, but rotation should happen when a session resumes — not proactively after every Stop. The design doc said "rotate per resume" but the Resume Detection section described "rotate per Stop," and the implementation followed the latter.

Claude Code provides a `SessionStart` hook that fires on `startup`, `resume`, `clear`, and `compact`. This is the correct place to detect "a new conversation segment is starting" and rotate the trace.

## Solution

Move trace rotation from `handleStop` to a new `handleSessionStart`. Stop continues to export spans using whatever epoch is current in state.

### SessionStart handler

When `CC_TRACE_ROTATE=true` and no `TRACEPARENT` is set:

- **New session** (no prior state): initialize at epoch 0, no rotation
- **Existing session** (has prior state): increment epoch, clear SessionSpanID

All four `source` values (`startup`, `resume`, `clear`, `compact`) trigger rotation on existing sessions. Per Claude Code docs, compact is "a new session with prepopulated context" — same trace rotation semantics as resume.

### Stop handler

Remove the rotation block (epoch increment + SessionSpanID clear). Export uses whatever epoch is in state.

### TRACEPARENT interaction

TRACEPARENT always wins. When set, SessionStart skips rotation entirely — the external trace owns the trace ID. This matches the existing behavior; it just moves from Stop to SessionStart.

| Scenario | SessionStart | Stop/Export |
|----------|-------------|-------------|
| No TRACEPARENT, rotate=true | Rotate (epoch++, clear SpanID) | Export with epoch-based trace ID |
| No TRACEPARENT, rotate=false | No-op | Export with base trace ID (epoch 0) |
| TRACEPARENT set, any rotate | No-op | Export as child of external trace |

`ExportSessionTrace` already handles TRACEPARENT via `effectiveRotate := rotate && !hasTraceparent` — no changes needed there.

## SessionStart payload

```json
{
  "session_id": "abc123",
  "transcript_path": "/Users/.../.claude/projects/.../00893aaf.jsonl",
  "cwd": "/Users/...",
  "permission_mode": "default",
  "hook_event_name": "SessionStart",
  "source": "resume",
  "model": "claude-sonnet-4-6"
}
```

Matcher values: `startup`, `resume`, `clear`, `compact`

## Hook configuration

Users add SessionStart to their `.claude/settings.json`:

```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "",
        "hooks": [{ "type": "command", "command": "~/.claude/hooks/cc-trace" }]
      }
    ]
  }
}
```

## Files changed

| File | Change |
|------|--------|
| `internal/hook/types.go` | Add `SessionStartPayload` struct |
| `cmd/cc-trace/main.go` | Add `handleSessionStart`, add `"SessionStart"` dispatch case, remove rotation from `handleStop` |
| `cmd/cc-trace/main_test.go` | Add SessionStart rotation tests, update Stop tests to remove rotation assertions |
| `CLAUDE.md` | Add SessionStart to hook events list |
| `README.md` | Add SessionStart to hook events, update setup instructions |

## Related

- Previous design: `docs/plans/2026-03-06-trace-rotation-design.md`
- SessionEnd cleanup: issue #23
- Docs: https://code.claude.com/docs/en/hooks#sessionstart
