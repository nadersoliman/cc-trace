package tracer

import (
	"context"
	"encoding/json"
	"fmt"

	"cc-trace/internal/hook"
	"cc-trace/internal/logging"

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

// matchSubagent finds a pending subagent whose execution window overlaps the Task tool call.
func matchSubagent(pending []hook.PendingSubagent, matched []bool, tc hook.ToolCall) (*hook.PendingSubagent, int) {
	for i, sub := range pending {
		if matched[i] || len(sub.Turns) == 0 {
			continue
		}
		subStart := sub.Turns[0].StartTime
		subEnd := sub.Turns[len(sub.Turns)-1].EndTime
		// Subagent execution should fall within the Task tool call window.
		if !subStart.Before(tc.StartTime) && !subEnd.After(tc.EndTime) {
			return &sub, i
		}
	}
	return nil, -1
}

// emitSubagentSpans creates child spans for a subagent under the Task tool span.
func emitSubagentSpans(tracer trace.Tracer, taskSpan trace.Span, turnCtx context.Context, sub hook.PendingSubagent) {
	taskCtx := trace.ContextWithSpan(turnCtx, taskSpan)

	subStart := sub.Turns[0].StartTime
	subEnd := sub.Turns[len(sub.Turns)-1].EndTime

	// Wrapper span for the subagent.
	subCtx, subSpan := tracer.Start(taskCtx, fmt.Sprintf("Subagent: %s", sub.AgentType),
		trace.WithTimestamp(subStart),
		trace.WithAttributes(
			attribute.String("agent.id", sub.AgentID),
			attribute.String("agent.type", sub.AgentType),
		),
	)

	// Create turn spans within the subagent.
	for _, subTurn := range sub.Turns {
		_, subTurnSpan := tracer.Start(subCtx, fmt.Sprintf("Turn %d", subTurn.Number),
			trace.WithTimestamp(subTurn.StartTime),
			trace.WithAttributes(
				attribute.Int("turn.number", subTurn.Number),
			),
		)
		subTurnCtx := trace.ContextWithSpan(subCtx, subTurnSpan)

		// LLM Response span.
		if subTurn.Model != "" {
			subLLMAttrs := []attribute.KeyValue{
				attribute.String("gen_ai.system", "anthropic"),
				attribute.String("gen_ai.request.model", subTurn.Model),
				attribute.Int("gen_ai.usage.input_tokens", subTurn.InputTokens),
				attribute.Int("gen_ai.usage.output_tokens", subTurn.OutputTokens),
				attribute.Int("gen_ai.usage.cache_read_tokens", subTurn.CacheReadTokens),
				attribute.Int("gen_ai.usage.cache_creation_tokens", subTurn.CacheCreationTokens),
			}
			if subTurn.StopReason != "" {
				subLLMAttrs = append(subLLMAttrs, attribute.String("gen_ai.response.finish_reason", subTurn.StopReason))
			}
			_, llmSpan := tracer.Start(subTurnCtx, "LLM Response",
				trace.WithTimestamp(subTurn.StartTime),
				trace.WithAttributes(subLLMAttrs...),
			)
			llmSpan.End(trace.WithTimestamp(subTurn.EndTime))
		}

		// Tool spans.
		for _, stc := range subTurn.ToolCalls {
			stcAttrs := []attribute.KeyValue{
				attribute.String("tool.name", stc.Name),
				attribute.String("tool.use_id", stc.ID),
				attribute.Bool("tool.success", stc.Success),
			}
			if stc.Input != nil {
				if inputJSON, err := json.Marshal(stc.Input); err == nil {
					stcAttrs = append(stcAttrs, attribute.String("tool.input", truncate(string(inputJSON), 4096)))
				}
			}

			_, stcSpan := tracer.Start(subTurnCtx, fmt.Sprintf("Tool: %s", stc.Name),
				trace.WithTimestamp(stc.StartTime),
				trace.WithAttributes(stcAttrs...),
			)
			if !stc.Success {
				stcSpan.SetStatus(codes.Error, "tool execution failed")
			}
			stcSpan.End(trace.WithTimestamp(stc.EndTime))
		}

		subTurnSpan.End(trace.WithTimestamp(subTurn.EndTime))
	}

	subSpan.End(trace.WithTimestamp(subEnd))
	logging.Debug(fmt.Sprintf("Emitted subagent %s (%s) with %d turns", sub.AgentType, sub.AgentID, len(sub.Turns)))
}
