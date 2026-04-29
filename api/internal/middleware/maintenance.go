// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// MaintenanceMode is an HTTP middleware that returns 503 Service Unavailable on
// MUTATING requests (POST/PUT/PATCH/DELETE) when env var LOOPZERO_MAINTENANCE_MODE
// is set to a non-empty value. Read-only requests (GET/HEAD/OPTIONS) and the
// health endpoint always pass through.
//
// Sprint 8 v3.2 / CHG-002a / D-7: required pre-flight before T0.1 (PG resize)
// to prevent in-flight mutations from observing partial database state during
// the ~5min managed-PG failover window. Operator sets LOOPZERO_MAINTENANCE_MODE
// before `pulumi up`, then unsets after PG online + smoke check passes.
//
// Response shape preserves the typed-error contract used elsewhere in the API
// (CLAUDE.md Rule 5: --json field names are stability contract):
//
//	{
//	  "code": "MAINTENANCE_MODE",
//	  "message": "<message>",
//	  "retry_after_seconds": <int>
//	}
//
// Retry-After header is also set so HTTP-aware clients honor backoff
// (CLI `transient.ts` parses this).
func MaintenanceMode(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mode := os.Getenv("LOOPZERO_MAINTENANCE_MODE")
		if mode == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Read-only verbs and the health endpoint always pass.
		method := r.Method
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/health") {
			next.ServeHTTP(w, r)
			return
		}
		// Internal routes (/internal/*) are called by platform components
		// (ArgoCD callbacks, bootstrap), not by users. Blocking them during
		// maintenance deadlocks in-flight deploys: the ArgoCD sync callback
		// never arrives, releases stay stuck in RELEASING until timeout.
		if strings.HasPrefix(r.URL.Path, "/internal/") {
			next.ServeHTTP(w, r)
			return
		}

		retryAfter := 300 // default 5 minutes — covers managed-PG failover window
		message := "LoopZero is in maintenance mode. Mutations are temporarily disabled while infrastructure is being resized. Try again shortly."
		if mode != "1" && mode != "true" {
			message = mode // operator-supplied human-readable message overrides default
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":                "MAINTENANCE_MODE",
			"message":             message,
			"retry_after_seconds": retryAfter,
		})
	})
}
