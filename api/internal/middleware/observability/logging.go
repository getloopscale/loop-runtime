// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package observability makes a request describable while it is being served.
//
// It carries the correlation id from the edge into the request context
// (correlation.go), builds the request-scoped logger every handler log line
// inherits from it (request_logger.go), opens the OpenTelemetry span
// (tracing.go), and writes the access log (this file). Nothing here decides
// whether a request proceeds — that is auth and policy. This package only
// records what happened.
//
// StatusWriter is exported rather than package-private because the audit
// middleware records the same response status the access log does, and after
// B-059 the two live in different packages.
package observability

import (
	"log/slog"
	"net/http"
	"time"
)

// StatusWriter wraps http.ResponseWriter to capture the status code.
// Exported because the audit middleware records the same status this
// package's request log does, and the two now live in different packages.
type StatusWriter struct {
	http.ResponseWriter
	status int
}

// NewStatusWriter wraps w, defaulting to 200 for a handler that writes a
// body without ever calling WriteHeader.
func NewStatusWriter(w http.ResponseWriter) *StatusWriter {
	return &StatusWriter{ResponseWriter: w, status: http.StatusOK}
}

// Status returns the code the handler wrote.
func (w *StatusWriter) Status() int { return w.status }

func (w *StatusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Flush delegates to the underlying ResponseWriter if it implements http.Flusher.
// This is required for SSE streaming to work through the logging middleware.
func (w *StatusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter, enabling http.ResponseController
// and other unwrapping mechanisms to discover interface implementations.
func (w *StatusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// RequestLogging returns middleware that logs every HTTP request.
// If logger is nil, logging is silently skipped (useful in tests).
func RequestLogging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := NewStatusWriter(w)

			next.ServeHTTP(sw, r)

			if logger != nil {
				logger.Info("request",
					"method", r.Method,
					"path", r.URL.Path,
					"status", sw.Status(),
					"duration_ms", time.Since(start).Milliseconds(),
					"correlation_id", r.Header.Get("Correlation-ID"),
				)
			}
		})
	}
}
