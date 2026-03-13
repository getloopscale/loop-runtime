package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// RequestLogging returns middleware that logs every HTTP request.
// If logger is nil, logging is silently skipped (useful in tests).
func RequestLogging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(sw, r)

			if logger != nil {
				logger.Info("request",
					"method", r.Method,
					"path", r.URL.Path,
					"status", sw.status,
					"duration_ms", time.Since(start).Milliseconds(),
					"correlation_id", r.Header.Get("Correlation-ID"),
				)
			}
		})
	}
}
