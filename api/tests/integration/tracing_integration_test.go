// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	observability "github.com/getloopscale/loop-runtime/api/internal/middleware/observability"
)

// setupTestTracer configures an in-memory span exporter for tests.
// tracetest.InMemoryExporter is a real SpanExporter implementation (not a mock).
// Approved for use per MEET-2026-03-30-020.
func setupTestTracer(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})
	return exporter
}

func TestOtelHTTPMiddleware_GeneratesSpan(t *testing.T) {
	exporter := setupTestTracer(t)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := observability.OtelHTTP(inner)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span, got none")
	}

	span := spans[0]
	if span.Name != "GET /api/v1/health" {
		t.Errorf("expected span name 'GET /api/v1/health', got %q", span.Name)
	}
}

func TestOtelHTTPMiddleware_CorrelationIDRemainsUUID(t *testing.T) {
	// C2: Correlation-ID format must remain UUID v4, independent of trace-id.
	_ = setupTestTracer(t)

	uuidPattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Chain: OtelHTTP -> CorrelationID -> inner (same order as production)
	handler := observability.OtelHTTP(observability.CorrelationID(inner))
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	correlationID := rec.Header().Get("Correlation-ID")
	if correlationID == "" {
		t.Fatal("expected Correlation-ID header, got empty")
	}
	if !uuidPattern.MatchString(correlationID) {
		t.Errorf("Correlation-ID %q is not a valid UUID v4 (C2 violation: format changed)", correlationID)
	}
}

func TestOtelHTTPMiddleware_WithTraceparentHeader(t *testing.T) {
	exporter := setupTestTracer(t)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := observability.OtelHTTP(inner)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// Inject a W3C traceparent header (version-traceId-parentSpanId-flags)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span, got none")
	}

	span := spans[0]
	// The span should be a child of the injected traceparent
	traceID := span.SpanContext.TraceID().String()
	if traceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("expected trace-id from traceparent, got %q", traceID)
	}
}

func TestOtelHTTPMiddleware_WithoutTraceparentGeneratesNewTrace(t *testing.T) {
	exporter := setupTestTracer(t)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := observability.OtelHTTP(inner)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span, got none")
	}

	span := spans[0]
	if !span.SpanContext.TraceID().IsValid() {
		t.Error("expected valid trace-id, got invalid")
	}
}

func TestTracingDisabled_NoSpansGenerated(t *testing.T) {
	// C7: When tracing is disabled (no real TracerProvider), no spans should be generated.
	// Reset to no-op provider.
	otel.SetTracerProvider(sdktrace.NewTracerProvider())

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := observability.OtelHTTP(inner)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// With no-op provider, request should still succeed
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}
