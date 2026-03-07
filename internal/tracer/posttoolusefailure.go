package tracer

import (
	"encoding/json"

	"cc-trace/internal/hook"

	"go.opentelemetry.io/otel/attribute"
)

// buildToolFailureAttrs builds span attributes for a failed tool call,
// enriching with PostToolUseFailure-specific fields from ToolSpanData.
func buildToolFailureAttrs(tc hook.ToolCall, tsd hook.ToolSpanData) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("tool.name", tc.Name),
		attribute.String("tool.use_id", tc.ID),
		attribute.Bool("tool.success", tc.Success),
	}
	if tc.Input != nil {
		if inputJSON, err := json.Marshal(tc.Input); err == nil {
			attrs = append(attrs, attribute.String("tool.input", truncate(string(inputJSON), 4096)))
		}
	}
	if tsd.Error != "" {
		attrs = append(attrs, attribute.String("error", tsd.Error))
	}
	attrs = append(attrs, attribute.Bool("is_interrupt", tsd.IsInterrupt))
	if tsd.HookEventName != "" {
		attrs = append(attrs, attribute.String("hook.event_name", tsd.HookEventName))
	}
	if tsd.PermissionMode != "" {
		attrs = append(attrs, attribute.String("permission_mode", tsd.PermissionMode))
	}
	if tsd.AgentID != "" {
		attrs = append(attrs, attribute.String("agent.id", tsd.AgentID))
	}
	if tsd.AgentType != "" {
		attrs = append(attrs, attribute.String("agent.type", tsd.AgentType))
	}
	return attrs
}
