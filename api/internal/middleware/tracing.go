// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
)

// OtelHTTP wraps HTTP handlers with OpenTelemetry instrumentation.
// Creates a span per request with standard HTTP attributes (method, route, status).
//
// MEET-2026-03-30-020 conditions:
//   - C2: Correlation-ID is NOT modified. trace_id is added as a SEPARATE
//     slog field alongside the existing correlation_id.
//   - Must be registered BEFORE CorrelationID middleware so the span context
//     is available for logging, but CorrelationID remains independent.
//
// When tracing is disabled (no TracerProvider registered), otelhttp produces
// no-op spans with zero overhead.
func OtelHTTP(next http.Handler) http.Handler {
	// Wrap next with a trace_id injector that runs INSIDE the otelhttp span.
	// otelhttp.NewHandler creates the span and passes it via context to `inner`.
	// The injector extracts trace_id from the span and adds it to the slog context
	// BEFORE downstream handlers execute, so all request logs include trace_id.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		if span.SpanContext().IsValid() {
			traceID := span.SpanContext().TraceID().String()
			// Add trace_id to the slog default handler for this request's context.
			// This does NOT modify Correlation-ID — trace_id is a separate field (C2).
			ctx := r.Context()
			logger := slog.Default().With("trace_id", traceID)
			ctx = contextWithLogger(ctx, logger)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})

	return otelhttp.NewHandler(inner, "loopzero-api",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)
}

type loggerCtxKey struct{}

// contextWithLogger stores a logger in context for trace_id propagation.
func contextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerCtxKey{}, logger)
}

