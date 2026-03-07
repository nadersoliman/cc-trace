# Spike: Resource Attributes vs Span Attributes

Date: 2026-03-07
Status: open

## Question

What are OTel resource attributes vs span attributes, what capabilities does each provide in Grafana Tempo, and how should cc-trace use them?

## Context

cc-trace already emits both attribute types but the split was made intuitively. As we consider enriching trace data (e.g., adding `session.epoch`, cost estimates, prompt metadata), we need a principled framework for where new attributes belong.

Current resource attributes: `service.name`, `hook.version`, `project.name`, `project.agent`
Current span attributes: `session.id`, `turn.number`, `turn.duration_ms`, `gen_ai.*`, `tool.*`, `user.prompt`, `user.prompt_length`

## Findings

### Resource Attributes

- Describe **who** is producing telemetry (the entity/environment)
- **Immutable** -- set once at SDK init, same for every span in a batch
- Sent **once per batch**, not per span -- cheaper storage and transmission
- Should have **low cardinality** (service name, version, host, project)
- Exempt from attribute count limits in the OTel spec
- TraceQL prefix: `resource.` (e.g., `resource.service.name = "project@obsidian"`)

### Span Attributes

- Describe **what** the operation is doing (the specific work)
- Set at **span creation** (or during span lifecycle)
- Sent **with every span** -- higher storage cost
- Can have **high cardinality** (user ID, prompt text, token counts, SQL queries)
- TraceQL prefix: `span.` (e.g., `span.turn.number > 10`)

### Key Differences

| Feature | Resource Attributes | Span Attributes |
|---|---|---|
| Describes | Who is producing telemetry | What the operation is doing |
| Scope | Entire service/process lifecycle | Single span |
| Mutability | Immutable, set once | Set at span creation |
| Cardinality | Low | High |
| Cost | Once per batch | Per span |
| TraceQL | `resource.` prefix | `span.` prefix |

### TraceQL Querying

Both types are first-class in Tempo's TraceQL:

```
// Resource-level filtering
{ resource.project.name = "obsidian" }

// Span-level filtering
{ span.turn.duration_ms > 60000 }

// Combined
{ resource.service.name = "project@obsidian" && span.turn.number > 10 }
```

Naming caveat: avoid naming span attributes with a `resource.` prefix -- it confuses the TraceQL parser.

### Storage Implications

Tempo deduplicates resource attributes in its Parquet storage -- stored once per batch, not per span. Moving stable data to resource attributes reduces storage. But if a value varies per span, it must be a span attribute.

## Implications

### Current split is correct

- Resource: `service.name`, `hook.version`, `project.*` -- stable per invocation
- Span: `turn.*`, `gen_ai.*`, `tool.*`, `user.*` -- varies per span

### Candidates for new resource attributes

- `session.id` -- currently a span attribute on the Session root, but it's the same for all spans in a batch. Moving it to resource would allow `resource.session.id` queries across all spans without needing the Session root span. Trade-off: loses the explicit "this is the session root" signal.

### Candidates for new span attributes

- `session.epoch` -- on Session root span, useful for correlating rotated traces
- `gen_ai.cost_estimate` -- on LLM Response spans, if we add cost tracking
- `turn.tool_count` -- on Turn spans, for quick filtering of complex turns

### Hook payload fields not yet captured

Examined a `PostToolUseFailure` dump. These fields are available but not yet on spans:

| Hook field | Proposed attribute | Span | Notes |
|---|---|---|---|
| `error` | `error` | Tool | Failure reason -- critical for debugging. Flat field, no domain prefix. |
| `tool_input` | `tool.input` | Tool | What the tool was asked to do. High cardinality, potentially sensitive. Consider truncation or a flag. |
| `permission_mode` | `permission_mode` | Turn or Session | Session's permission level. Flat field. |
| `is_interrupt` | `is_interrupt` | Tool | Whether user interrupted. Flat field -- not scoped to tool, it's a top-level event property. |
| `hook_event_name` | `hook.event_name` | Tool | Which hook fired (PostToolUse vs PostToolUseFailure). Domain-prefixed. |

See `internal/tracer/CLAUDE.md` for the attribute naming convention that governs how hook payload fields map to span attribute names.

### Rule of thumb for cc-trace

**Resource**: doesn't change within a single hook invocation (service, project, hook version, host)
**Span**: specific to the operation (turn number, model, tokens, tool name, prompt, duration)
