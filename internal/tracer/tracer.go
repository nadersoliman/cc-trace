package tracer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"cc-trace/internal/hook"
	"cc-trace/internal/logging"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// traceIDFromSession generates a deterministic trace ID from a session ID.
func traceIDFromSession(sessionID string) trace.TraceID {
	h := sha256.Sum256([]byte(sessionID))
	var tid trace.TraceID
	copy(tid[:], h[:16])
	return tid
}

// InitTracer sets up the OTel TracerProvider with OTLP HTTP exporter.
//
// All configuration is read from standard OTel environment variables:
//   - OTEL_EXPORTER_OTLP_ENDPOINT (default: http://localhost:4318)
//   - OTEL_SERVICE_NAME (default: unknown_service)
//   - OTEL_RESOURCE_ATTRIBUTES (default: none)
func InitTracer() (func(), error) {
	ctx := context.Background()

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithInsecure(),
		otlptracehttp.WithTimeout(5*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("create exporter: %w", err)
	}

	return InitTracerWithExporter(exporter)
}

// InitTracerWithExporter sets up the OTel TracerProvider with the given exporter.
// This allows tests to inject an in-memory exporter instead of a real OTLP one.
func InitTracerWithExporter(exporter sdktrace.SpanExporter) (func(), error) {
	ctx := context.Background()

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithAttributes(
			attribute.String("hook.version", "0.1.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tp.Shutdown(ctx)
	}
	return shutdown, nil
}

// ExportSessionTrace creates all spans for a session and exports them.
func ExportSessionTrace(sessionID string, turns []hook.Turn, toolSpanData []hook.ToolSpanData, pendingSubagents []hook.PendingSubagent, ss *hook.SessionState) {
	tracer := otel.Tracer("cc-trace")

	if len(turns) == 0 {
		logging.Debug("No turns to export")
		return
	}

	sessionEnd := turns[len(turns)-1].EndTime

	// Track which subagents have been matched to avoid double-use.
	matched := make([]bool, len(pendingSubagents))

	// Determine the base trace context.
	// If TRACEPARENT is set, the session becomes a child of the external trace.
	// Otherwise, use a deterministic trace ID from the session ID (standalone mode).
	var baseCtx context.Context
	if parentCtx, ok := parseTraceparent(); ok {
		baseCtx = parentCtx
		logging.Debug("Using TRACEPARENT from environment for parent context")
	} else {
		tid := traceIDFromSession(sessionID)
		baseCtx = contextWithTraceID(tid)
	}

	// Build the session context for turn spans.
	// First Stop: create a new Session root span and persist its SpanID.
	// Subsequent Stops: reuse the stored SpanID as remote parent (no new Session span).
	var sessionCtx context.Context
	var sessionSpan trace.Span
	if ss.SessionSpanID == "" {
		sessionStart := turns[0].StartTime
		sessionCtx, sessionSpan = tracer.Start(baseCtx, fmt.Sprintf("Session %s", truncate(sessionID, 12)),
			trace.WithTimestamp(sessionStart),
			trace.WithAttributes(
				attribute.String("session.id", sessionID),
			),
		)
		ss.SessionSpanID = sessionSpan.SpanContext().SpanID().String()
		ss.SessionStart = sessionStart
		logging.Debug(fmt.Sprintf("Created Session span %s for session %s", ss.SessionSpanID, truncate(sessionID, 12)))
	} else {
		traceID := trace.SpanContextFromContext(baseCtx).TraceID()
		sidBytes, err := hex.DecodeString(ss.SessionSpanID)
		if err != nil || len(sidBytes) != 8 {
			logging.Log("ERROR", fmt.Sprintf("Invalid stored SessionSpanID: %s", ss.SessionSpanID))
			return
		}
		var sid trace.SpanID
		copy(sid[:], sidBytes)
		sc := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceID,
			SpanID:     sid,
			TraceFlags: trace.FlagsSampled,
			Remote:     true,
		})
		sessionCtx = trace.ContextWithRemoteSpanContext(context.Background(), sc)
		logging.Debug(fmt.Sprintf("Reusing Session span %s for session %s", ss.SessionSpanID, truncate(sessionID, 12)))
	}

	// Create turn spans as children of session.
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

	// End session span only if it was created in this invocation.
	if sessionSpan != nil {
		sessionSpan.End(trace.WithTimestamp(sessionEnd))
	}
	logging.Debug(fmt.Sprintf("Exported %d turns for session %s", len(turns), truncate(sessionID, 12)))
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

// parseTraceparent reads TRACEPARENT from the environment and returns a context
// with the parent span context. Returns (ctx, true) if valid, (nil, false) otherwise.
// Format: "00-<trace_id_hex32>-<parent_span_id_hex16>-<flags_hex2>"
func parseTraceparent() (context.Context, bool) {
	tp := os.Getenv("TRACEPARENT")
	if tp == "" {
		return nil, false
	}

	parts := strings.Split(tp, "-")
	if len(parts) != 4 || parts[0] != "00" {
		logging.Debug(fmt.Sprintf("Invalid TRACEPARENT format: %s", tp))
		return nil, false
	}

	tidBytes, err := hex.DecodeString(parts[1])
	if err != nil || len(tidBytes) != 16 {
		logging.Debug(fmt.Sprintf("Invalid TRACEPARENT trace_id: %s", parts[1]))
		return nil, false
	}

	sidBytes, err := hex.DecodeString(parts[2])
	if err != nil || len(sidBytes) != 8 {
		logging.Debug(fmt.Sprintf("Invalid TRACEPARENT span_id: %s", parts[2]))
		return nil, false
	}

	var tid trace.TraceID
	var sid trace.SpanID
	copy(tid[:], tidBytes)
	copy(sid[:], sidBytes)

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    tid,
		SpanID:     sid,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	return trace.ContextWithRemoteSpanContext(context.Background(), sc), true
}

// contextWithTraceID creates a context with a pre-set trace ID using a custom span context.
func contextWithTraceID(tid trace.TraceID) context.Context {
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    tid,
		TraceFlags: trace.FlagsSampled,
	})
	return trace.ContextWithRemoteSpanContext(context.Background(), sc)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
