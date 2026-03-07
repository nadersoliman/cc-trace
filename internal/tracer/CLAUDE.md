# internal/tracer/CLAUDE.md

OTel span creation and export. This package is where all span attributes are defined.

## Attribute Naming Convention

Follows [OTel semantic conventions](https://opentelemetry.io/docs/specs/semconv/general/attributes/): dots for namespace hierarchy, underscores for compound words within a level.

### Rules

1. **Domain-prefixed fields** -- if the first segment is a domain noun shared by other fields (`tool_`, `agent_`, `session_`, `hook_`, `turn_`, `gen_ai_`), the first `_` becomes `.`
   - `tool_name` -> `tool.name`
   - `agent_type` -> `agent.type`
   - `session_id` -> `session.id`
   - `hook_event_name` -> `hook.event_name`

2. **Flat fields** -- if the field has no domain prefix, keep it as-is. Do not force a namespace.
   - `cwd` -> `cwd`
   - `error` -> `error`
   - `is_interrupt` -> `is_interrupt`
   - `permission_mode` -> `permission_mode`

3. **Compound words within a level** stay underscore-separated
   - `tool.use_id` (not `tool.use.id`)
   - `turn.stop_reason` (not `turn.stop.reason`)
   - `gen_ai.usage.cache_read_tokens`

### The test

> Does the first segment represent a domain noun that other fields share?

If yes, it's a namespace -- split at the first `_` to `.`. If no, keep flat.

### Respect payload semantics

The attribute name should reflect the **original scope** of the data. Don't re-scope fields to fit a namespace. `is_interrupt` is a top-level event property, not scoped to `tool`. `cwd` is the working directory, not scoped to `session`.

## Current Attribute Map

### Resource attributes (set once per batch via OTEL_RESOURCE_ATTRIBUTES)

| Attribute | Source |
|-----------|--------|
| `service.name` | `OTEL_SERVICE_NAME` env var |
| `hook.version` | Hardcoded in binary |
| `project.name` | `OTEL_RESOURCE_ATTRIBUTES` |
| `project.agent` | `OTEL_RESOURCE_ATTRIBUTES` |

### Span attributes

| Span | Attribute | Type | Source |
|------|-----------|------|--------|
| Session | `session.id` | string | Hook payload `session_id` |
| Turn | `turn.number` | int | Transcript parsing |
| Turn | `turn.duration_ms` | int | Transcript parsing |
| Turn | `turn.stop_reason` | string | Transcript parsing |
| Turn | `user.prompt` | string | Transcript parsing (truncated to 500 chars) |
| Turn | `user.prompt_length` | int | Transcript parsing |
| LLM Response | `gen_ai.system` | string | Hardcoded `"anthropic"` |
| LLM Response | `gen_ai.request.model` | string | Transcript parsing |
| LLM Response | `gen_ai.usage.input_tokens` | int | Transcript parsing |
| LLM Response | `gen_ai.usage.output_tokens` | int | Transcript parsing |
| LLM Response | `gen_ai.usage.cache_read_tokens` | int | Transcript parsing |
| LLM Response | `gen_ai.usage.cache_creation_tokens` | int | Transcript parsing |
| LLM Response | `gen_ai.response.finish_reason` | string | Transcript parsing |
| Tool | `tool.name` | string | Hook payload `tool_name` |
| Tool | `tool.use_id` | string | Hook payload `tool_use_id` |
| Tool | `tool.success` | bool | Hook event type |
| Subagent | `agent.type` | string | Hook payload `agent_type` |
| Subagent | `agent.id` | string | Hook payload `agent_id` |
