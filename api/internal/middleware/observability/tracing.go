// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package observability

import (
	"context"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
)

// OtelHTTP wraps HTTP handlers with OpenTelemetry instrumentation.
// Creates a span per request with standard HTTP attributes (method, route, status).
//
// MEET-2026-03-30-020 conditions:
//   - C2: Correlation-ID is NOT modified. trace_id is a SEPARATE slog field
//     alongside correlation_id.
//   - Must be registered BEFORE CorrelationID and RequestLogger so the span is
//     in the request context when RequestLogger enriches the request-scoped
//     logger through TraceIDs.
//
// When tracing is disabled (no TracerProvider registered), otelhttp produces
// no-op spans with zero overhead, and TraceIDs reports none.
func OtelHTTP(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "loopruntime-api",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)
}

// TraceIDs is the logging.TraceExtractor for OpenTelemetry: the trace and span
// ids of the span active in ctx, or empty strings when there is none. Passed to
// logging.NewContextLogger, it is what puts trace_id on every request log line.
func TraceIDs(ctx context.Context) (traceID, spanID string) {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return "", ""
	}
	return sc.TraceID().String(), sc.SpanID().String()
}
