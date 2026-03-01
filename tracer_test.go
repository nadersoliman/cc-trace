package main

import (
	"testing"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestInitTracerWithExporter(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	shutdown, err := initTracerWithExporter(exporter)
	if err != nil {
		t.Fatalf("initTracerWithExporter failed: %v", err)
	}
	defer shutdown()

	spans := exporter.GetSpans()
	if len(spans) != 0 {
		t.Errorf("expected 0 spans initially, got %d", len(spans))
	}
}
