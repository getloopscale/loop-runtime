// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// auditBodySnoopLimit caps the bytes the middleware will buffer to look up
// project_id for resource_id enrichment. Bodies larger than this skip
// enrichment (resource_id stays empty). 64 KiB covers all known mutation
// payloads (deploy, secret, domain) with two orders of magnitude headroom.
const auditBodySnoopLimit = 64 * 1024

// projectIDBodyKeys is the ordered list of JSON keys the audit middleware
// scans for in mutation request bodies to populate resource_id. The order
// matches API frequency: builds use "project_id", domains use "project".
var projectIDBodyKeys = []string{"project_id", "project", "projectId"}

// AuditWriter writes audit entries to persistent storage.
// Decoupled from db package to avoid import cycles.
type AuditWriter func(tenantID, action, actor, resourceType, resourceID, correlationID string, metadata map[string]any)

// AuditLog returns middleware that logs all mutation requests.
// Writes to slog always. If auditWriter is non-nil, also writes to DB.
//
// resource_id enrichment (CHG-2026-05-06-AUDIT-RESOURCE-ID): for mutation
// requests where the resource identifier is not present in the URL path
// (POST /builds, POST /secrets, POST /domains), the middleware also looks
// at the JSON request body's project_id/project field and the ?project=
// query string, populating resource_id with the most specific value found.
// This restores the audit-forensics invariant that build.submitted /
// secret.set / domain.added entries can be traced back to a project.
func AuditLog(logger *slog.Logger, auditWriter AuditWriter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// Snoop the JSON request body once (capped) so we can populate
			// resource_id from project_id/project fields. The body is
			// restored before the handler runs so downstream JSON decoders
			// see exactly what the client sent.
			var bodySnapshot []byte
			if shouldSnoopBody(r) {
				bodySnapshot = readAndRestoreBody(r)
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
				resourceID := extractResourceID(r.URL.Path)
				if resourceID == "" {
					resourceID = enrichResourceIDFromRequest(r, bodySnapshot)
				}
				auditWriter(
					tenantID,
					deriveAction(r.Method, r.URL.Path),
					userID,
					extractResourceType(r.URL.Path),
					resourceID,
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

// shouldSnoopBody reports whether the audit middleware should buffer the
// request body for resource_id enrichment. Only mutation methods with a
// JSON content-type and a bounded length qualify.
func shouldSnoopBody(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
	default:
		return false
	}
	if r.Body == nil || r.Body == http.NoBody {
		return false
	}
	if r.ContentLength <= 0 || r.ContentLength > auditBodySnoopLimit {
		// ContentLength == -1 is "unknown" — skip rather than gamble.
		return false
	}
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		return false
	}
	// Accept "application/json" and "application/json; charset=utf-8" etc.
	for i := 0; i < len(ct); i++ {
		if ct[i] == ';' || ct[i] == ' ' {
			ct = ct[:i]
			break
		}
	}
	return ct == "application/json"
}

// readAndRestoreBody reads up to auditBodySnoopLimit bytes from r.Body and
// installs a fresh ReadCloser so the next handler sees the exact same bytes.
// Returns the captured snapshot (possibly nil on read error).
func readAndRestoreBody(r *http.Request) []byte {
	limited := io.LimitReader(r.Body, auditBodySnoopLimit+1)
	buf, err := io.ReadAll(limited)
	if err != nil || len(buf) > auditBodySnoopLimit {
		// On error or if the body exceeded the cap (we read +1 to detect),
		// rebuild the body from what we managed to read concatenated with the
		// remainder of the original — so handlers still see complete data.
		// Snoop is opt-out: we return nil so resource_id stays "" rather than
		// risk a partial extraction.
		original := r.Body
		r.Body = struct {
			io.Reader
			io.Closer
		}{Reader: io.MultiReader(bytes.NewReader(buf), original), Closer: original}
		return nil
	}
	r.Body = io.NopCloser(bytes.NewReader(buf))
	return buf
}

// enrichResourceIDFromRequest looks for a project identifier in the request
// body and query string when the URL path did not yield one. This closes the
// audit-forensics gap for endpoints whose resource lives in the body
// (POST /builds, POST /domains) or query (POST /secrets).
func enrichResourceIDFromRequest(r *http.Request, body []byte) string {
	// 1. Query string (?project=...) — used by POST/DELETE /api/v1/secrets.
	if v := r.URL.Query().Get("project"); v != "" {
		return v
	}
	if v := r.URL.Query().Get("project_id"); v != "" {
		return v
	}
	// 2. JSON body — POST /api/v1/builds (project_id), POST /api/v1/domains (project).
	if len(body) == 0 {
		return ""
	}
	var generic map[string]any
	if err := json.Unmarshal(body, &generic); err != nil {
		return ""
	}
	for _, key := range projectIDBodyKeys {
		if v, ok := generic[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
