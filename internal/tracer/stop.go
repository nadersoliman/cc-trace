package tracer

import (
	"context"
	"fmt"

	"cc-trace/internal/hook"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// createTurnSpans creates turn, LLM, tool, and subagent spans as children of the session span.
func createTurnSpans(tracer trace.Tracer, sessionCtx context.Context, turns []hook.Turn, toolSpanData []hook.ToolSpanData, pendingSubagents []hook.PendingSubagent, matched []bool) {
	for _, turn := range turns {
		turnAttrs := []attribute.KeyValue{
			attribute.Int("turn.number", turn.Number),
			attribute.String("user.prompt", truncate(turn.UserText, 500)),
			attribute.Int("user.prompt_length", len(turn.UserText)),
		}
		if turn.DurationMs > 0 {
			turnAttrs = append(turnAttrs, attribute.Int("turn.duration_ms", turn.DurationMs))
		}
		if turn.StopReason != "" {
			turnAttrs = append(turnAttrs, attribute.String("turn.stop_reason", turn.StopReason))
		}
		_, turnSpan := tracer.Start(sessionCtx, fmt.Sprintf("Turn %d", turn.Number),
			trace.WithTimestamp(turn.StartTime),
			trace.WithAttributes(turnAttrs...),
		)

		turnCtx := trace.ContextWithSpan(sessionCtx, turnSpan)

		// LLM Response span.
		if turn.Model != "" {
			llmAttrs := []attribute.KeyValue{
				attribute.String("gen_ai.system", "anthropic"),
				attribute.String("gen_ai.request.model", turn.Model),
				attribute.Int("gen_ai.usage.input_tokens", turn.InputTokens),
				attribute.Int("gen_ai.usage.output_tokens", turn.OutputTokens),
				attribute.Int("gen_ai.usage.cache_read_tokens", turn.CacheReadTokens),
				attribute.Int("gen_ai.usage.cache_creation_tokens", turn.CacheCreationTokens),
			}
			if turn.StopReason != "" {
				llmAttrs = append(llmAttrs, attribute.String("gen_ai.response.finish_reason", turn.StopReason))
			}
			_, llmSpan := tracer.Start(turnCtx, "LLM Response",
				trace.WithTimestamp(turn.StartTime),
				trace.WithAttributes(llmAttrs...),
			)
			llmSpan.End(trace.WithTimestamp(turn.EndTime))
		}

		// Tool spans.
		for _, tc := range turn.ToolCalls {
			attrs := buildToolAttrs(tc, toolSpanData)

			_, toolSpan := tracer.Start(turnCtx, fmt.Sprintf("Tool: %s", tc.Name),
				trace.WithTimestamp(tc.StartTime),
				trace.WithAttributes(attrs...),
			)
			if !tc.Success {
				toolSpan.SetStatus(codes.Error, "tool execution failed")
			}

			// For Task tool calls, nest matching subagent spans before ending.
			if tc.Name == "Task" {
				if sub, idx := matchSubagent(pendingSubagents, matched, tc); sub != nil {
					matched[idx] = true
					emitSubagentSpans(tracer, toolSpan, turnCtx, *sub)
				}
			}

			toolSpan.End(trace.WithTimestamp(tc.EndTime))
		}

		turnSpan.End(trace.WithTimestamp(turn.EndTime))
	}
}
