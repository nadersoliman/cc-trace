---
title: "fix: Relay OTEL_* env vars via CC_TRACE_OTEL_* prefix"
type: fix
status: active
date: 2026-05-19
---

# fix: Relay OTEL_* env vars via CC_TRACE_OTEL_* prefix

## Overview

Claude Code v2.1.128 intentionally strips all `OTEL_*` env vars from hook subprocesses to prevent OTel-instrumented apps from inheriting the CLI's telemetry config. cc-trace, as a hook, loses its export configuration. Fix by relaying via `CC_TRACE_OTEL_*` prefix, which passes through Claude Code's subprocess allowlist.

## Problem Frame

cc-trace hooks run to completion and log `[INFO] Exported N turns` but no spans arrive at the collector. The OTel SDK falls back to `http://localhost:4318/v1/traces` (its built-in default) because `OTEL_EXPORTER_OTLP_ENDPOINT` is absent from the hook subprocess environment.

Root cause: Claude Code v2.1.128 (May 4, 2026) strips `OTEL_*` from all subprocesses (Bash, hooks, MCP, LSP). `CC_TRACE_*` and `CLAUDE_CODE_*` prefixes pass through. This is intentional, documented in the [release notes](https://github.com/anthropics/claude-code/releases/tag/v2.1.128).

The `CLAUDE_ENV_FILE` mechanism mentioned in issue #30 is a write-back channel for hooks to inject vars into subsequent Bash tool commands — not a delivery mechanism for hook config. Sourcing it is not the correct fix.

## Requirements Trace

- R1. cc-trace must receive OTEL export configuration despite the v2.1.128 strip
- R2. Users must be able to set `CC_TRACE_OTEL_*` vars in `settings.json` `env:` and have them reach the OTel SDK
- R3. Direct `OTEL_*` env vars (if somehow present) must take precedence over relayed values
- R4. The relay must work for any `OTEL_*` var without a hardcoded whitelist
- R5. Debug logging from #31 must surface the relay activity

## Scope Boundaries

- No `CLAUDE_ENV_FILE` parsing — research confirmed it's not the right mechanism
- No upstream changes to Claude Code — this is a cc-trace-side workaround
- No new dependencies

### Deferred to Separate Tasks

- Upstream issue on `anthropics/claude-code` requesting hook exemption from `OTEL_*` strip: separate issue

## Key Technical Decisions

- **Mechanical prefix strip over whitelist**: Any env var matching `CC_TRACE_OTEL_*` gets `CC_TRACE_` stripped to produce the corresponding `OTEL_*` var. Predictable, zero maintenance, forwards-compatible with new OTel vars.
- **Direct env wins**: Only set via `os.Setenv` if `os.LookupEnv` returns false for the target key. This ensures that if `OTEL_*` vars are somehow present (older Claude Code, wrapper script, CI), they take precedence.
- **Relay runs once at startup, before InitTracer**: The relay is a one-shot env preparation step in `main()`, not a persistent mechanism.

## Minimum User Configuration

For cc-trace to export to a remote collector, the user needs at minimum:

```jsonc
// ~/.claude/settings.json
{
  "env": {
    "CC_TRACE_OTEL_EXPORTER_OTLP_ENDPOINT": "https://otel.example.com",
    "CC_TRACE_OTEL_EXPORTER_OTLP_HEADERS": "Authorization=Bearer xxx"
  }
}
```

Optional enrichment (sensible defaults exist):
- `CC_TRACE_OTEL_SERVICE_NAME` (default: `unknown_service`)
- `CC_TRACE_OTEL_RESOURCE_ATTRIBUTES` (default: none)
- `CC_TRACE_OTEL_EXPORTER_OTLP_PROTOCOL` (default: `http/protobuf`)

## Implementation Units

- [x] **Unit 1: Add relay function and tests**

**Goal:** Implement `relayOtelEnv()` that scans `os.Environ()` for `CC_TRACE_OTEL_*` vars, strips the `CC_TRACE_` prefix, and sets the corresponding `OTEL_*` var if not already present.

**Requirements:** R1, R2, R3, R4

**Dependencies:** None

**Files:**
- Modify: `cmd/cc-trace/main.go`
- Modify: `cmd/cc-trace/main_test.go`

**Approach:**
- New function `relayOtelEnv() int` in `main.go` that returns the count of vars relayed
- Scan `os.Environ()` for entries starting with `CC_TRACE_OTEL_`
- For each match: strip `CC_TRACE_` prefix to get target key, check `os.LookupEnv(target)`, set only if absent
- Call from `main()` after logging init but before any code that reads `OTEL_*` vars

**Patterns to follow:**
- Existing env var reading pattern in `main()` lines 32-35 (`os.Getenv` + `strings.EqualFold`)
- `os.LookupEnv` for presence check (distinguishes empty from absent)

**Test scenarios:**
- Happy path: `CC_TRACE_OTEL_EXPORTER_OTLP_ENDPOINT=https://example.com` is relayed to `OTEL_EXPORTER_OTLP_ENDPOINT`
- Happy path: Multiple `CC_TRACE_OTEL_*` vars are all relayed
- Edge case: Direct `OTEL_EXPORTER_OTLP_ENDPOINT` already set — relay does NOT overwrite (R3)
- Edge case: `CC_TRACE_DEBUG=true` is NOT relayed (doesn't match `CC_TRACE_OTEL_` prefix)
- Edge case: `CC_TRACE_OTEL_` with empty value — still set (empty is a valid value, distinct from absent)
- Edge case: No `CC_TRACE_OTEL_*` vars present — clean no-op, returns 0

**Verification:**
- All tests pass
- `relayOtelEnv` correctly distinguishes `CC_TRACE_OTEL_*` from `CC_TRACE_*`

---

- [x] **Unit 2: Integrate relay into main() and add debug logging**

**Goal:** Wire `relayOtelEnv()` into the startup path and log relay activity via the existing debug logging infrastructure.

**Requirements:** R5

**Dependencies:** Unit 1

**Files:**
- Modify: `cmd/cc-trace/main.go`

**Approach:**
- Call `relayOtelEnv()` in `main()` after `logging.Init` and before stdin read / event dispatch
- Log one debug line: `"Relayed %d CC_TRACE_OTEL_* vars to OTEL_*"` (or `"No CC_TRACE_OTEL_* vars to relay"` when count is 0)
- The existing `logExportConfig()` in `InitTracer` (from #31) will then show the resolved values — no duplication needed

**Patterns to follow:**
- Existing `logging.Debug()` calls in `main.go`

**Test scenarios:**
- Test expectation: none — integration is verified by Unit 1 tests and the existing #31 debug logging tests

**Verification:**
- With `CC_TRACE_DEBUG=true`, the relay log line appears before the `InitTracer` config dump
- The `InitTracer` debug output shows the relayed endpoint, not the localhost default

---

- [x] **Unit 3: Update documentation**

**Goal:** Document the `CC_TRACE_OTEL_*` relay in README and CLAUDE.md.

**Requirements:** R1, R2

**Dependencies:** Unit 1

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`

**Approach:**
- Add a section explaining the relay mechanism and why it exists (v2.1.128 strips `OTEL_*`)
- Update the environment variables table to show `CC_TRACE_OTEL_*` as the recommended way to pass OTEL config
- Document minimum configuration (endpoint + headers)
- Remove or update the `CLAUDE_ENV_FILE` workaround from issue #30 if present

**Test scenarios:**
- Test expectation: none — documentation only

**Verification:**
- README clearly explains the minimum setup for a new user

## System-Wide Impact

- **Interaction graph:** The relay runs once in `main()` before any OTel SDK initialization. It affects `tracer.InitTracer()` which reads `OTEL_*` from env. The #31 `logExportConfig()` will naturally surface the relayed values.
- **Error propagation:** No new error paths — `os.Setenv` doesn't fail in practice.
- **Unchanged invariants:** All existing `CC_TRACE_*` vars (`DEBUG`, `TIMING`, `DUMP`, `ROTATE`) are unaffected — the relay only matches the `CC_TRACE_OTEL_` prefix, not `CC_TRACE_` broadly.

## Risks & Dependencies

| Risk | Mitigation |
|------|------------|
| Future Claude Code version strips `CC_TRACE_*` too | Unlikely — `CC_TRACE_*` is a user-defined namespace, not an OTel convention. If it happens, upstream issue is the fix. |
| User sets both `CC_TRACE_OTEL_*` and `OTEL_*` (via wrapper) | Direct `OTEL_*` wins per R3. No conflict. |

## Sources & References

- Related issue: #30 (this PR closes it)
- Related PR: #32 (debug logging that surfaced the root cause)
- Claude Code v2.1.128 release: https://github.com/anthropics/claude-code/releases/tag/v2.1.128
- Claude Code OTel docs: https://code.claude.com/docs/en/agent-sdk/observability
