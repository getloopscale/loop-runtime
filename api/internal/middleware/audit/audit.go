// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package audit

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	"github.com/getloopscale/loop-runtime/api/internal/middleware/observability"
)

// AuditWriter writes audit entries to persistent storage.
// Decoupled from db package to avoid import cycles.
type AuditWriter func(tenantID, action, actor, resourceType, resourceID, correlationID string, metadata map[string]any)

// AuditLog returns middleware that logs all mutation requests.
// Writes to slog always. If auditWriter is non-nil, also writes to DB.
//
// resource_id derivation (B-059, supersedes CHG-2026-05-06-AUDIT-RESOURCE-ID):
// the id comes from the handler's declaration first, then from the URL path,
// then nowhere. It is never guessed from the request body or the query string.
//
// The superseded mechanism buffered up to 64 KiB of every mutating body and
// scanned it for a hand-maintained list of JSON keys ("project_id", "project",
// "projectId"). That put per-domain knowledge in the transport: a domain that
// spelt its id a fourth way lost `resource_id` silently, which is the worst
// failure mode audit data has. Handlers know their own ids; they declare them
// through `audit.SetResourceID`, and this middleware only records.
func AuditLog(logger *slog.Logger, auditWriter AuditWriter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// Install the declaration slot on the way in so the handler below
			// can fill it; read it on the way out. The slot travels by pointer,
			// which is what lets the value move UP the chain.
			r = r.WithContext(WithResourceIDSlot(r.Context()))

			start := time.Now()
			wrapped := observability.NewStatusWriter(w)
			next.ServeHTTP(wrapped, r)

			fields := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", wrapped.Status(),
				"duration_ms", time.Since(start).Milliseconds(),
				"remote_addr", r.RemoteAddr,
			}

			uc := auth.GetUserContext(r.Context())
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
				action, resourceType := resolveIdentity(r)
				auditWriter(
					tenantID,
					action,
					userID,
					resourceType,
					resolveResourceID(r),
					correlationID,
					map[string]any{
						"status":      wrapped.Status(),
						"method":      r.Method,
						"path":        r.URL.Path,
						"duration_ms": time.Since(start).Milliseconds(),
					},
				)
			}
		})
	}
}

// resolveIdentity returns the action and resource type the route declared
// (Declare). A mutation that reached the middleware undeclared is recorded as
// "undeclared.<METHOD>" / "undeclared" — a bounded name with no path segment
// or id in it, so it never multiplies audit cardinality — and warned about;
// the server tests require every mounted mutation to declare.
func resolveIdentity(r *http.Request) (action, resourceType string) {
	action, resourceType = declaredIdentity(r.Context())
	if action != "" {
		return action, resourceType
	}
	slog.Warn("audit: mutation without a declared audit identity (mount audit.Declare on the route)",
		"method", r.Method, "path", r.URL.Path)
	return "undeclared." + r.Method, "undeclared"
}

// resolveResourceID applies the B-059 fallback order: what the handler declared,
// then what the router bound into the URL, then nothing.
//
// The URL stays as a fallback because the router — not a guess — put the id
// there. Anything beyond these two is guessing, and guessing is what this slice
// removed.
func resolveResourceID(r *http.Request) string {
	if declared := GetResourceID(r.Context()); declared != "" {
		return declared
	}
	return extractResourceID(r.URL.Path)
}
