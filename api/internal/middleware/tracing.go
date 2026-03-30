// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
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
	wrapped := otelhttp.NewHandler(next, "loopzero-api",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wrapped.ServeHTTP(w, r)

		// After otelhttp processes the request, extract trace_id for structured logging.
		// This does NOT modify Correlation-ID — trace_id is a separate field (C2).
		span := trace.SpanFromContext(r.Context())
		if span.SpanContext().IsValid() {
			slog.DebugContext(r.Context(), "trace context active",
				"trace_id", span.SpanContext().TraceID().String(),
			)
		}
	})
}
