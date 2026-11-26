// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metrics

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// metricsWriter wraps http.ResponseWriter to capture the status code for metrics.
type metricsWriter struct {
	http.ResponseWriter
	status int
}

func (w *metricsWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Flush delegates to the underlying ResponseWriter if it implements http.Flusher.
func (w *metricsWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter.
func (w *metricsWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// HTTPMetrics returns chi middleware that records request duration, count, and in-flight gauges.
//
// Path labels use the chi route pattern (e.g. "/api/v1/builds/{id}") to keep cardinality bounded.
// Without this, each unique build ID would create a separate time series — a cardinality bomb.
func HTTPMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HTTPRequestsInFlight.Inc()
		defer HTTPRequestsInFlight.Dec()

		start := time.Now()
		mw := &metricsWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(mw, r)

		duration := time.Since(start).Seconds()
		status := fmt.Sprintf("%d", mw.status)

		// Use chi's route pattern for bounded cardinality.
		// Falls back to the raw path only for unmatched routes (404s).
		pattern := r.URL.Path
		if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.RoutePattern() != "" {
			pattern = rctx.RoutePattern()
		}

		HTTPRequestDuration.WithLabelValues(r.Method, pattern, status).Observe(duration)
		HTTPRequestsTotal.WithLabelValues(r.Method, pattern, status).Inc()
	})
}
