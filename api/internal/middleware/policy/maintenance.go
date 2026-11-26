// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package policy

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// MaintenanceMode is an HTTP middleware that returns 503 Service Unavailable on
// MUTATING requests (POST/PUT/PATCH/DELETE) while the maintenance setting
// (LOOP_RUNTIME_MAINTENANCE_MODE, see ParseMaintenanceSetting) is on. Read-only requests (GET/HEAD/OPTIONS) and the
// health endpoint always pass through.
//
// Sprint 8 v3.2 / CHG-002a / D-7: required pre-flight before T0.1 (PG resize)
// to prevent in-flight mutations from observing partial database state during
// the ~5min managed-PG failover window. Operator sets LOOP_RUNTIME_MAINTENANCE_MODE
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
func MaintenanceMode(setting MaintenanceSetting) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return maintenanceGate(setting, next)
	}
}

// MaintenanceSetting is LOOP_RUNTIME_MAINTENANCE_MODE, parsed once at boot.
type MaintenanceSetting struct {
	Enabled bool
	Message string // shown to refused callers
}

const defaultMaintenanceMessage = "LoopRuntime is in maintenance mode. Mutations are temporarily disabled while infrastructure is being resized. Try again shortly."

// ParseMaintenanceSetting reads a LOOP_RUNTIME_MAINTENANCE_MODE value. Empty, "0",
// "false", "off" and "no" (any case) mean off — the published chart renders
// "false" by default, so reading every non-empty value as "on" put a default
// install into maintenance. "1", "true", "on" and "yes" mean on with the
// default message; any other value means on, with that value as the message
// shown to callers.
func ParseMaintenanceSetting(raw string) MaintenanceSetting {
	v := strings.TrimSpace(raw)
	switch strings.ToLower(v) {
	case "", "0", "false", "off", "no":
		return MaintenanceSetting{}
	case "1", "true", "on", "yes":
		return MaintenanceSetting{Enabled: true, Message: defaultMaintenanceMessage}
	}
	return MaintenanceSetting{Enabled: true, Message: v}
}

func maintenanceGate(setting MaintenanceSetting, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !setting.Enabled {
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

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":                "MAINTENANCE_MODE",
			"message":             setting.Message,
			"retry_after_seconds": retryAfter,
		})
	})
}
