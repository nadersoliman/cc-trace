package tracer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"cc-trace/internal/hook"
	"cc-trace/internal/logging"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// traceIDFromSession generates a deterministic trace ID from a session ID.
// When epoch > 0, the trace ID includes the epoch to produce a unique trace per resume.
func traceIDFromSession(sessionID string, epoch int) trace.TraceID {
	input := sessionID
	if epoch > 0 {
		input = fmt.Sprintf("%s:%d", sessionID, epoch)
	}
	h := sha256.Sum256([]byte(input))
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
		otlptracehttp.WithTimeout(60*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("create exporter: %w", err)
	}

	shutdown, _, err := InitTracerWithExporter(exporter)
	return shutdown, err
}

// InitTracerWithExporter sets up the OTel TracerProvider with the given exporter.
// This allows tests to inject an in-memory exporter instead of a real OTLP one.
//
// Returns (shutdown, flush, error):
//   - shutdown: flushes pending spans and shuts down the provider
//   - flush: exports pending spans without shutting down (for tests)
func InitTracerWithExporter(exporter sdktrace.SpanExporter) (func(), func(), error) {
	ctx := context.Background()

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithAttributes(
			attribute.String("hook.version", "0.1.0"),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create resource: %w", err)
	}

	bsp := sdktrace.NewBatchSpanProcessor(exporter)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(bsp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	shutdown := func() {
		_ = tp.Shutdown(context.Background())
	}
	flush := func() {
		_ = tp.ForceFlush(context.Background())
	}
	return shutdown, flush, nil
}

// ExportSessionTrace creates all spans for a session and exports them.
// When rotate is true, each invocation creates a new trace (epoch-based trace ID)
// and a fresh Session root span, producing self-contained traces per conversation.
func ExportSessionTrace(sessionID string, turns []hook.Turn, toolSpanData []hook.ToolSpanData, pendingSubagents []hook.PendingSubagent, ss *hook.SessionState, rotate bool) {
	tracer := otel.Tracer("cc-trace")

	if len(turns) == 0 {
		logging.Debug("No turns to export")
		return
	}

	sessionEnd := turns[len(turns)-1].EndTime

	// Track which subagents have been matched to avoid double-use.
	matched := make([]bool, len(pendingSubagents))

	// Determine the base trace context.
	// If TRACEPARENT is set, the session becomes a child of the external trace
	// and rotation is suppressed — the external trace owns the trace ID.
	// Otherwise, use a deterministic trace ID from the session ID (standalone mode).
	var baseCtx context.Context
	var hasTraceparent bool
	if parentCtx, ok := parseTraceparent(); ok {
		baseCtx = parentCtx
		hasTraceparent = true
		logging.Debug("Using TRACEPARENT from environment for parent context")
	} else {
		tid := traceIDFromSession(sessionID, ss.Epoch)
		baseCtx = contextWithTraceID(tid)
	}

	// Rotation only applies in standalone mode (no TRACEPARENT).
	// When an external trace context is provided, the caller owns the trace ID.
	effectiveRotate := rotate && !hasTraceparent

	// Build the session context for turn spans.
	// When rotating, always create a fresh Session root span per conversation.
	// Otherwise: first Stop creates a new span, subsequent Stops reuse the stored SpanID.
	var sessionCtx context.Context
	var sessionSpan trace.Span
	if ss.SessionSpanID == "" || effectiveRotate {
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

	// Delegate span creation to stop.go.
	createTurnSpans(tracer, sessionCtx, turns, toolSpanData, pendingSubagents, matched)

	// End session span only if it was created in this invocation.
	if sessionSpan != nil {
		sessionSpan.End(trace.WithTimestamp(sessionEnd))
	}
	logging.Debug(fmt.Sprintf("Exported %d turns for session %s", len(turns), truncate(sessionID, 12)))
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
