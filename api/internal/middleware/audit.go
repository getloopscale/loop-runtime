// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

// AuditWriter writes audit entries to persistent storage.
// Decoupled from db package to avoid import cycles.
type AuditWriter func(tenantID, action, actor, resourceType, resourceID, correlationID string, metadata map[string]any)

// AuditLog returns middleware that logs all mutation requests.
// Writes to slog always. If auditWriter is non-nil, also writes to DB.
func AuditLog(logger *slog.Logger, auditWriter AuditWriter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			wrapped := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(wrapped, r)

			fields := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", wrapped.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote_addr", r.RemoteAddr,
			}

			uc := GetUserContext(r.Context())
			var tenantID, userID string
			if uc != nil {
				tenantID = uc.TenantID
				userID = uc.UserID
				fields = append(fields, "user_id", uc.UserID, "tenant_id", uc.TenantID)
			}

			correlationID := r.Header.Get("Correlation-ID")
			if correlationID == "" {
				correlationID = r.Header.Get("X-Request-Id")
			}
			if correlationID != "" {
				fields = append(fields, "correlation_id", correlationID)
			}

			logger.Info("audit", fields...)

			// Write to audit_log table (fire-and-forget)
			if auditWriter != nil && tenantID != "" {
				auditWriter(
					tenantID,
					deriveAction(r.Method, r.URL.Path),
					userID,
					extractResourceType(r.URL.Path),
					extractResourceID(r.URL.Path),
					correlationID,
					map[string]any{
						"status":      wrapped.status,
						"method":      r.Method,
						"path":        r.URL.Path,
						"duration_ms": time.Since(start).Milliseconds(),
					},
				)
			}
		})
	}
}
