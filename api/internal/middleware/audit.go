package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// AuditLog returns middleware that logs all mutation (non-GET/HEAD/OPTIONS) requests
// with structured fields for observability and compliance tracing.
// It captures: method, path, status, duration, user identity, and correlation ID.
func AuditLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip read-only methods — only audit mutations.
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()

			// Reuse statusWriter from logging.go to capture response status code.
			wrapped := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(wrapped, r)

			fields := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", wrapped.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote_addr", r.RemoteAddr,
			}

			// Attach user identity when available (after auth middleware).
			uc := GetUserContext(r.Context())
			if uc != nil {
				fields = append(fields,
					"user_id", uc.UserID,
					"tenant_id", uc.TenantID,
				)
			}

			correlationID := r.Header.Get("Correlation-ID")
			if correlationID != "" {
				fields = append(fields, "correlation_id", correlationID)
			}

			logger.Info("audit", fields...)
		})
	}
}
