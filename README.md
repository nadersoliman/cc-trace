# cc-trace

[![CI](https://github.com/nadersoliman/cc-trace/actions/workflows/ci.yml/badge.svg)](https://github.com/nadersoliman/cc-trace/actions/workflows/ci.yml)
[![Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/nadersoliman/6e75df2dcb94824b03b934effe541426/raw/coverage.json)](https://github.com/nadersoliman/cc-trace/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/nadersoliman/cc-trace)](https://goreportcard.com/report/github.com/nadersoliman/cc-trace)
[![Lines of Code](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/nadersoliman/6e75df2dcb94824b03b934effe541426/raw/loc.json)](https://github.com/nadersoliman/cc-trace)
[![Test Ratio](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/nadersoliman/6e75df2dcb94824b03b934effe541426/raw/test-ratio.json)](https://github.com/nadersoliman/cc-trace)
[![Go Version](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/nadersoliman/6e75df2dcb94824b03b934effe541426/raw/go-version.json)](https://github.com/nadersoliman/cc-trace)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

OpenTelemetry trace hook for [Claude Code](https://docs.anthropic.com/en/docs/claude-code). Turns your sessions into structured OTel traces — every turn, LLM response, tool call, and subagent gets its own span.

## What You Get

```
Session Root
  └── Turn N
       ├── LLM Response (model, tokens)
       ├── Tool: Read
       ├── Tool: Task
       │    └── Subagent: general-purpose
       │         └── Turn 1
       │              ├── LLM Response
       │              └── Tool: Grep
       └── Tool: Edit
```

## Prerequisites

- Go 1.21+
- An OTLP-compatible collector ([Grafana Alloy](https://grafana.com/docs/alloy/), [Jaeger](https://www.jaegertracing.io/), [OTel Collector](https://opentelemetry.io/docs/collector/), etc.)

## Install

```bash
git clone https://github.com/nadersoliman/cc-trace.git
cd cc-trace
make install
```

This builds the binary and copies it to `~/.claude/hooks/cc-trace`.

## Configure

Add to your global Claude Code settings (`~/.claude/settings.json`):

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

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4318` | OTLP endpoint -- `http://` or `https://` (appends `/v1/traces`) |
| `OTEL_SERVICE_NAME` | `unknown_service` | `service.name` resource attribute |
| `OTEL_RESOURCE_ATTRIBUTES` | — | Comma-separated `key=value` pairs (e.g. `project.name=myapp`) |
| `OTEL_EXPORTER_OTLP_HEADERS` | — | Comma-separated `key=value` headers for OTLP requests (e.g. `Authorization=Bearer xxx`). Read automatically by the OTel SDK |
| `CC_TRACE_DEBUG` | `false` | Debug log to `~/.claude/state/cc_trace.log` |
| `CC_TRACE_TIMING` | `false` | Phase-level timing logs to `~/.claude/state/cc_trace.log` |
| `CC_TRACE_DUMP` | `false` | Dump raw hook payloads and transcripts to `/tmp/cc-trace/dumps/` |
| `CC_TRACE_ROTATE` | `false` | Rotate trace ID per session segment. Each SessionStart (startup/resume/clear/compact) on an existing session creates a new trace, preventing long-lived sessions from outliving backend retention. Search by `session.id` attribute to find all segments. Ignored when `TRACEPARENT` is set -- the external trace owns the trace ID. |
| `TRACEPARENT` | — | [W3C Trace Context](https://www.w3.org/TR/trace-context/) parent. When set, the session trace becomes a child of the external trace (e.g. a CI pipeline span). Format: `00-<trace_id>-<span_id>-<flags>` |

Set these per-project in `.claude/settings.json` under `"env"`, or export them in your shell:

```jsonc
// .claude/settings.json (project-level)
{
  "env": {
    "OTEL_SERVICE_NAME": "my-project",
    "OTEL_RESOURCE_ATTRIBUTES": "project.name=my-project"
  }
}
```

## How It Works

Short-lived Go CLI invoked by Claude Code via stdin JSON. **SessionStart** rotates the trace when `CC_TRACE_ROTATE` is enabled. **PostToolUse** / **PostToolUseFailure** and **SubagentStop** record data locally with zero network calls (< 10ms). On **Stop**, the hook parses the session transcript, builds the span tree, and exports via OTLP/HTTP.
