package tracer

import (
	"encoding/json"

	"cc-trace/internal/hook"

	"go.opentelemetry.io/otel/attribute"
)

// buildToolAttrs builds the span attributes for a successful tool call.
func buildToolAttrs(tc hook.ToolCall, toolSpanData []hook.ToolSpanData) []attribute.KeyValue {
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
	// Enrich from PostToolUse data if available.
	for _, tsd := range toolSpanData {
		if tsd.ToolUseID == tc.ID && tsd.ToolResponse != nil {
			if respJSON, err := json.Marshal(tsd.ToolResponse); err == nil {
				attrs = append(attrs, attribute.String("tool.response", truncate(string(respJSON), 4096)))
			}
			break
		}
	}
	return attrs
}
