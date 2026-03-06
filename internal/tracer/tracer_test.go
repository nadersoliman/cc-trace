package tracer

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cc-trace/internal/hook"
	"cc-trace/internal/logging"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestInitTracerWithExporter(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	shutdown, _, err := InitTracerWithExporter(exporter)
	if err != nil {
		t.Fatalf("InitTracerWithExporter failed: %v", err)
	}
	defer shutdown()

	spans := exporter.GetSpans()
	if len(spans) != 0 {
		t.Errorf("expected 0 spans initially, got %d", len(spans))
	}
}

// --- Helper functions ---

// setupTestTracer creates an InMemoryExporter and wires it as the global tracer.
func setupTestTracer(t *testing.T) (*tracetest.InMemoryExporter, func(), func()) {
	t.Helper()
	logging.Init(filepath.Join(t.TempDir(), "test.log"), false)
	exporter := tracetest.NewInMemoryExporter()
	shutdown, flush, err := InitTracerWithExporter(exporter)
	if err != nil {
		t.Fatalf("InitTracerWithExporter: %v", err)
	}
	return exporter, shutdown, flush
}

// findSpan finds a span by exact name.
func findSpan(spans tracetest.SpanStubs, name string) *tracetest.SpanStub {
	for i := range spans {
		if spans[i].Name == name {
			return &spans[i]
		}
	}
	return nil
}

// findSpans finds all spans matching a name prefix.
func findSpans(spans tracetest.SpanStubs, prefix string) []tracetest.SpanStub {
	var result []tracetest.SpanStub
	for _, s := range spans {
		if len(s.Name) >= len(prefix) && s.Name[:len(prefix)] == prefix {
			result = append(result, s)
		}
	}
	return result
}

// getAttr finds an attribute value by key.
func getAttr(span *tracetest.SpanStub, key string) interface{} {
	for _, a := range span.Attributes {
		if string(a.Key) == key {
			return a.Value.AsInterface()
		}
	}
	return nil
}

// --- Tests ---

func TestTraceIDDeterministic(t *testing.T) {
	sessionA := "session-abc-123"
	sessionB := "session-xyz-789"

	tidA1 := traceIDFromSession(sessionA, 0)
	tidA2 := traceIDFromSession(sessionA, 0)
	tidB := traceIDFromSession(sessionB, 0)

	if tidA1 != tidA2 {
		t.Errorf("same session ID produced different trace IDs: %s vs %s", tidA1, tidA2)
	}

	if tidA1 == tidB {
		t.Errorf("different session IDs produced same trace ID: %s", tidA1)
	}
}

func TestTraceparentParsing(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		t.Setenv("TRACEPARENT", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
		ctx, ok := parseTraceparent()
		if !ok {
			t.Fatal("expected ok=true for valid TRACEPARENT")
		}
		if ctx == nil {
			t.Fatal("expected non-nil context for valid TRACEPARENT")
		}
	})

	t.Run("missing", func(t *testing.T) {
		t.Setenv("TRACEPARENT", "")
		_, ok := parseTraceparent()
		if ok {
			t.Fatal("expected ok=false for missing TRACEPARENT")
		}
	})

	t.Run("invalid_format", func(t *testing.T) {
		t.Setenv("TRACEPARENT", "not-a-valid-traceparent")
		_, ok := parseTraceparent()
		if ok {
			t.Fatal("expected ok=false for invalid TRACEPARENT format")
		}
	})
}

func TestExportSessionTrace_SingleTurn(t *testing.T) {
	exporter, shutdown, flush := setupTestTracer(t)
	defer shutdown()

	sessionID := "test-session-single-turn"
	now := time.Now()

	turns := []hook.Turn{
		{
			Number:       1,
			Model:        "claude-sonnet-4-20250514",
			InputTokens:  100,
			OutputTokens: 20,
			StopReason:   "end_turn",
			StartTime:    now,
			EndTime:      now.Add(2 * time.Second),
		},
	}

	ss := &hook.SessionState{}
	ExportSessionTrace(sessionID, turns, nil, nil, ss, false)

	flush()
	spans := exporter.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("expected 3 spans (Session, Turn, LLM Response), got %d", len(spans))
	}

	// Session span
	sessionSpans := findSpans(spans, "Session")
	if len(sessionSpans) != 1 {
		t.Fatalf("expected 1 Session span, got %d", len(sessionSpans))
	}
	sessionSpan := &sessionSpans[0]
	if !strings.HasPrefix(sessionSpan.Name, "Session") {
		t.Errorf("session span name should start with 'Session', got %q", sessionSpan.Name)
	}
	if v := getAttr(sessionSpan, "session.id"); v != sessionID {
		t.Errorf("session.id = %v, want %s", v, sessionID)
	}

	// Turn span
	turnSpan := findSpan(spans, "Turn 1")
	if turnSpan == nil {
		t.Fatal("expected 'Turn 1' span")
	}
	// Turn should be a child of Session
	if turnSpan.Parent.SpanID() != sessionSpan.SpanContext.SpanID() {
		t.Errorf("Turn 1 parent span ID = %s, want Session span ID %s",
			turnSpan.Parent.SpanID(), sessionSpan.SpanContext.SpanID())
	}

	// LLM Response span
	llmSpan := findSpan(spans, "LLM Response")
	if llmSpan == nil {
		t.Fatal("expected 'LLM Response' span")
	}
	// LLM Response should be a child of Turn
	if llmSpan.Parent.SpanID() != turnSpan.SpanContext.SpanID() {
		t.Errorf("LLM Response parent span ID = %s, want Turn span ID %s",
			llmSpan.Parent.SpanID(), turnSpan.SpanContext.SpanID())
	}

	// Verify LLM attributes
	if v := getAttr(llmSpan, "gen_ai.request.model"); v != "claude-sonnet-4-20250514" {
		t.Errorf("gen_ai.request.model = %v, want claude-sonnet-4-20250514", v)
	}
	if v := getAttr(llmSpan, "gen_ai.usage.input_tokens"); v != int64(100) {
		t.Errorf("gen_ai.usage.input_tokens = %v, want 100", v)
	}
	if v := getAttr(llmSpan, "gen_ai.response.finish_reason"); v != "end_turn" {
		t.Errorf("gen_ai.response.finish_reason = %v, want end_turn", v)
	}

	// Verify SessionSpanID is set
	if ss.SessionSpanID == "" {
		t.Error("expected ss.SessionSpanID to be set after export")
	}
}

func TestExportSessionTrace_WithTools(t *testing.T) {
	exporter, shutdown, flush := setupTestTracer(t)
	defer shutdown()

	sessionID := "test-session-with-tools"
	now := time.Now()

	turns := []hook.Turn{
		{
			Number:    1,
			Model:     "claude-sonnet-4-20250514",
			StartTime: now,
			EndTime:   now.Add(3 * time.Second),
			ToolCalls: []hook.ToolCall{
				{
					Name:      "Read",
					ID:        "toolu_test_001",
					Success:   true,
					StartTime: now.Add(500 * time.Millisecond),
					EndTime:   now.Add(1 * time.Second),
				},
			},
		},
	}

	ss := &hook.SessionState{}
	ExportSessionTrace(sessionID, turns, nil, nil, ss, false)

	flush()
	spans := exporter.GetSpans()

	// Should have: Session, Turn 1, LLM Response, Tool: Read
	toolSpan := findSpan(spans, "Tool: Read")
	if toolSpan == nil {
		t.Fatal("expected 'Tool: Read' span")
	}

	// Tool should be a child of Turn 1
	turnSpan := findSpan(spans, "Turn 1")
	if turnSpan == nil {
		t.Fatal("expected 'Turn 1' span")
	}
	if toolSpan.Parent.SpanID() != turnSpan.SpanContext.SpanID() {
		t.Errorf("Tool: Read parent span ID = %s, want Turn span ID %s",
			toolSpan.Parent.SpanID(), turnSpan.SpanContext.SpanID())
	}

	// Verify tool attributes
	if v := getAttr(toolSpan, "tool.name"); v != "Read" {
		t.Errorf("tool.name = %v, want Read", v)
	}
	if v := getAttr(toolSpan, "tool.success"); v != true {
		t.Errorf("tool.success = %v, want true", v)
	}
}

func TestExportSessionTrace_WithSubagent(t *testing.T) {
	exporter, shutdown, flush := setupTestTracer(t)
	defer shutdown()

	sessionID := "test-session-with-subagent"
	now := time.Now()

	taskStart := now.Add(500 * time.Millisecond)
	taskEnd := now.Add(5 * time.Second)
	subStart := now.Add(1 * time.Second)
	subEnd := now.Add(4 * time.Second)

	turns := []hook.Turn{
		{
			Number:    1,
			Model:     "claude-sonnet-4-20250514",
			StartTime: now,
			EndTime:   now.Add(6 * time.Second),
			ToolCalls: []hook.ToolCall{
				{
					Name:      "Task",
					ID:        "toolu_task_001",
					Success:   true,
					StartTime: taskStart,
					EndTime:   taskEnd,
				},
			},
		},
	}

	pendingSubagents := []hook.PendingSubagent{
		{
			AgentID:   "agent-001",
			AgentType: "general-purpose",
			Turns: []hook.Turn{
				{
					Number:       1,
					Model:        "claude-sonnet-4-20250514",
					InputTokens:  50,
					OutputTokens: 10,
					StartTime:    subStart,
					EndTime:      subEnd,
					ToolCalls: []hook.ToolCall{
						{
							Name:      "Grep",
							ID:        "toolu_sub_001",
							Success:   true,
							StartTime: subStart.Add(200 * time.Millisecond),
							EndTime:   subStart.Add(800 * time.Millisecond),
						},
					},
				},
			},
		},
	}

	ss := &hook.SessionState{}
	ExportSessionTrace(sessionID, turns, nil, pendingSubagents, ss, false)

	flush()
	spans := exporter.GetSpans()

	// Expect Subagent: general-purpose span
	subagentSpan := findSpan(spans, "Subagent: general-purpose")
	if subagentSpan == nil {
		t.Fatal("expected 'Subagent: general-purpose' span")
	}

	// Verify subagent attributes
	if v := getAttr(subagentSpan, "agent.type"); v != "general-purpose" {
		t.Errorf("agent.type = %v, want general-purpose", v)
	}
	if v := getAttr(subagentSpan, "agent.id"); v != "agent-001" {
		t.Errorf("agent.id = %v, want agent-001", v)
	}
}

func TestExportSessionTrace_IncrementalExport(t *testing.T) {
	exporter, shutdown, flush := setupTestTracer(t)
	defer shutdown()

	sessionID := "test-session-incremental"
	now := time.Now()

	// First export: turn 1
	turns1 := []hook.Turn{
		{
			Number:    1,
			Model:     "claude-sonnet-4-20250514",
			StartTime: now,
			EndTime:   now.Add(2 * time.Second),
		},
	}

	ss := &hook.SessionState{}
	ExportSessionTrace(sessionID, turns1, nil, nil, ss, false)

	flush()
	savedSpanID := ss.SessionSpanID
	if savedSpanID == "" {
		t.Fatal("expected SessionSpanID to be set after first export")
	}

	// Reset exporter for second export
	exporter.Reset()

	// Second export: turn 2 with same session state
	turns2 := []hook.Turn{
		{
			Number:    2,
			Model:     "claude-sonnet-4-20250514",
			StartTime: now.Add(5 * time.Second),
			EndTime:   now.Add(7 * time.Second),
		},
	}

	ExportSessionTrace(sessionID, turns2, nil, nil, ss, false)

	flush()

	// SessionSpanID should be unchanged
	if ss.SessionSpanID != savedSpanID {
		t.Errorf("SessionSpanID changed from %s to %s", savedSpanID, ss.SessionSpanID)
	}

	// No new Session spans in second export
	spans := exporter.GetSpans()
	sessionSpans := findSpans(spans, "Session")
	if len(sessionSpans) != 0 {
		t.Errorf("expected 0 Session spans in incremental export, got %d", len(sessionSpans))
	}
}

func TestExportSessionTrace_SpanAttributes(t *testing.T) {
	exporter, shutdown, flush := setupTestTracer(t)
	defer shutdown()

	sessionID := "test-session-attributes"
	now := time.Now()

	turns := []hook.Turn{
		{
			Number:              1,
			Model:               "claude-sonnet-4-20250514",
			InputTokens:         200,
			OutputTokens:        50,
			CacheReadTokens:     50,
			CacheCreationTokens: 10,
			StopReason:          "end_turn",
			DurationMs:          5000,
			StartTime:           now,
			EndTime:             now.Add(5 * time.Second),
		},
	}

	ss := &hook.SessionState{}
	ExportSessionTrace(sessionID, turns, nil, nil, ss, false)

	flush()
	spans := exporter.GetSpans()

	// Verify Turn span attributes
	turnSpan := findSpan(spans, "Turn 1")
	if turnSpan == nil {
		t.Fatal("expected 'Turn 1' span")
	}
	if v := getAttr(turnSpan, "turn.number"); v != int64(1) {
		t.Errorf("turn.number = %v, want 1", v)
	}
	if v := getAttr(turnSpan, "turn.duration_ms"); v != int64(5000) {
		t.Errorf("turn.duration_ms = %v, want 5000", v)
	}
	if v := getAttr(turnSpan, "turn.stop_reason"); v != "end_turn" {
		t.Errorf("turn.stop_reason = %v, want end_turn", v)
	}

	// Verify LLM Response span attributes
	llmSpan := findSpan(spans, "LLM Response")
	if llmSpan == nil {
		t.Fatal("expected 'LLM Response' span")
	}
	if v := getAttr(llmSpan, "gen_ai.system"); v != "anthropic" {
		t.Errorf("gen_ai.system = %v, want anthropic", v)
	}
	if v := getAttr(llmSpan, "gen_ai.usage.cache_read_tokens"); v != int64(50) {
		t.Errorf("gen_ai.usage.cache_read_tokens = %v, want 50", v)
	}
	if v := getAttr(llmSpan, "gen_ai.usage.cache_creation_tokens"); v != int64(10) {
		t.Errorf("gen_ai.usage.cache_creation_tokens = %v, want 10", v)
	}
}

func TestBatchFlush_SpansVisibleAfterFlush(t *testing.T) {
	logging.Init(filepath.Join(t.TempDir(), "test.log"), false)
	exporter := tracetest.NewInMemoryExporter()
	shutdown, flush, err := InitTracerWithExporter(exporter)
	if err != nil {
		t.Fatalf("InitTracerWithExporter: %v", err)
	}
	defer shutdown()

	// Create a span
	sessionID := "test-batch-flush"
	now := time.Now()
	turns := []hook.Turn{
		{
			Number:    1,
			Model:     "claude-sonnet-4-20250514",
			StartTime: now,
			EndTime:   now.Add(1 * time.Second),
		},
	}
	ss := &hook.SessionState{}
	ExportSessionTrace(sessionID, turns, nil, nil, ss, false)

	// Flush must be called to see spans with batch processor
	flush()

	spans := exporter.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("expected 3 spans after flush, got %d", len(spans))
	}
}

func TestTraceIDWithEpoch(t *testing.T) {
	sessionID := "test-session-epoch"

	// Epoch 0 produces a valid, deterministic trace ID.
	tid0a := traceIDFromSession(sessionID, 0)
	tid0b := traceIDFromSession(sessionID, 0)
	if tid0a != tid0b {
		t.Errorf("epoch 0 should be deterministic")
	}

	// Different epochs produce different trace IDs.
	tid1 := traceIDFromSession(sessionID, 1)
	tid2 := traceIDFromSession(sessionID, 2)

	if tid0a == tid1 {
		t.Errorf("epoch 0 and 1 should produce different trace IDs")
	}
	if tid1 == tid2 {
		t.Errorf("epoch 1 and 2 should produce different trace IDs")
	}

	// Same epoch is deterministic.
	tid1b := traceIDFromSession(sessionID, 1)
	if tid1 != tid1b {
		t.Errorf("same epoch should produce same trace ID")
	}
}

func TestExportSessionTrace_Rotation(t *testing.T) {
	exporter, shutdown, flush := setupTestTracer(t)
	defer shutdown()

	sessionID := "test-session-rotation"
	now := time.Now()

	// First conversation: epoch 0
	turns1 := []hook.Turn{
		{
			Number:    1,
			Model:     "claude-sonnet-4-20250514",
			StartTime: now,
			EndTime:   now.Add(2 * time.Second),
		},
	}

	ss := &hook.SessionState{}
	ExportSessionTrace(sessionID, turns1, nil, nil, ss, true)
	flush()

	spans1 := exporter.GetSpans()
	// Should have Session + Turn + LLM Response
	if len(spans1) != 3 {
		t.Fatalf("first export: expected 3 spans, got %d", len(spans1))
	}
	firstTraceID := spans1[0].SpanContext.TraceID()
	firstSessionSpanID := ss.SessionSpanID
	if firstSessionSpanID == "" {
		t.Fatal("expected SessionSpanID to be set after first export")
	}

	// Simulate handleStop: increment epoch, clear span ID
	ss.Epoch++
	ss.SessionSpanID = ""

	exporter.Reset()

	// Second conversation: epoch 1
	turns2 := []hook.Turn{
		{
			Number:    2,
			Model:     "claude-sonnet-4-20250514",
			StartTime: now.Add(10 * time.Second),
			EndTime:   now.Add(12 * time.Second),
		},
	}

	ExportSessionTrace(sessionID, turns2, nil, nil, ss, true)
	flush()

	spans2 := exporter.GetSpans()
	// Should have NEW Session + Turn + LLM Response
	if len(spans2) != 3 {
		t.Fatalf("second export: expected 3 spans, got %d", len(spans2))
	}
	secondTraceID := spans2[0].SpanContext.TraceID()

	// Trace IDs must differ between epochs.
	if firstTraceID == secondTraceID {
		t.Errorf("rotated trace IDs should differ: both are %s", firstTraceID)
	}

	// New Session span should exist.
	sessionSpans := findSpans(spans2, "Session")
	if len(sessionSpans) != 1 {
		t.Fatalf("expected 1 Session span in rotated export, got %d", len(sessionSpans))
	}

	// Session span should have same session.id attribute.
	if v := getAttr(&sessionSpans[0], "session.id"); v != sessionID {
		t.Errorf("session.id = %v, want %s", v, sessionID)
	}
}

func TestExportSessionTrace_TraceparentSuppressesRotation(t *testing.T) {
	exporter, shutdown, flush := setupTestTracer(t)
	defer shutdown()

	// Set TRACEPARENT -- rotation should be suppressed.
	t.Setenv("TRACEPARENT", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")

	sessionID := "test-session-traceparent-rotation"
	now := time.Now()

	// First export with rotate=true but TRACEPARENT set.
	turns1 := []hook.Turn{
		{
			Number:    1,
			Model:     "claude-sonnet-4-20250514",
			StartTime: now,
			EndTime:   now.Add(2 * time.Second),
		},
	}

	ss := &hook.SessionState{}
	ExportSessionTrace(sessionID, turns1, nil, nil, ss, true)
	flush()

	spans1 := exporter.GetSpans()
	if len(spans1) != 3 {
		t.Fatalf("first export: expected 3 spans, got %d", len(spans1))
	}
	firstTraceID := spans1[0].SpanContext.TraceID()
	savedSpanID := ss.SessionSpanID

	exporter.Reset()

	// Second export: same session state (caller would NOT increment epoch
	// because TRACEPARENT is set), rotate=true still passed but should be ignored.
	turns2 := []hook.Turn{
		{
			Number:    2,
			Model:     "claude-sonnet-4-20250514",
			StartTime: now.Add(5 * time.Second),
			EndTime:   now.Add(7 * time.Second),
		},
	}

	ExportSessionTrace(sessionID, turns2, nil, nil, ss, true)
	flush()

	spans2 := exporter.GetSpans()

	// Trace ID should be the TRACEPARENT's trace ID, same both times.
	secondTraceID := spans2[0].SpanContext.TraceID()
	if firstTraceID != secondTraceID {
		t.Errorf("TRACEPARENT should keep same trace ID: got %s and %s", firstTraceID, secondTraceID)
	}

	// SessionSpanID should be reused (no rotation), so no new Session span.
	if ss.SessionSpanID != savedSpanID {
		t.Errorf("SessionSpanID should be reused under TRACEPARENT: was %s, now %s", savedSpanID, ss.SessionSpanID)
	}
	sessionSpans := findSpans(spans2, "Session")
	if len(sessionSpans) != 0 {
		t.Errorf("expected 0 Session spans (reusing existing), got %d", len(sessionSpans))
	}
}
