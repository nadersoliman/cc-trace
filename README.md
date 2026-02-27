# cc-trace

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

This builds the binary and copies it to `~/.claude/hooks/otel_trace_hook`.

## Configure

Add to your global Claude Code settings (`~/.claude/settings.json`):

```jsonc
{
  "hooks": {
    "PostToolUse": [{ "hooks": [{ "type": "command", "command": "~/.claude/hooks/otel_trace_hook" }] }],
    "PostToolUseFailure": [{ "hooks": [{ "type": "command", "command": "~/.claude/hooks/otel_trace_hook" }] }],
    "SubagentStop": [{ "hooks": [{ "type": "command", "command": "~/.claude/hooks/otel_trace_hook" }] }],
    "Stop": [{ "hooks": [{ "type": "command", "command": "~/.claude/hooks/otel_trace_hook" }] }]
  }
}
```

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4318` | OTLP/HTTP endpoint (appends `/v1/traces`) |
| `OTEL_SERVICE_NAME` | `unknown_service` | `service.name` resource attribute |
| `OTEL_RESOURCE_ATTRIBUTES` | — | Comma-separated `key=value` pairs (e.g. `project.name=myapp`) |
| `CC_OTEL_TRACE_DEBUG` | `false` | Debug log to `~/.claude/state/otel_trace_hook.log` |
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

Short-lived Go CLI invoked by Claude Code via stdin JSON. **PostToolUse** / **PostToolUseFailure** and **SubagentStop** record data locally with zero network calls (< 10ms). On **Stop**, the hook parses the session transcript, builds the span tree, and exports via OTLP/HTTP.
