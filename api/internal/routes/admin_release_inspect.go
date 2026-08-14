// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// admin_release_inspect.go implements T4.4 of plan
// 2026-05-28-meet-001-engineering-closure-plan.md.
//
// GET /api/v1/admin/releases/{id}/inspect
//
// Gated behind LOOPZERO_ADMIN_API=true. Requires X-Service-Auth header
// matching LOOPZERO_SERVICE_AUTH_SECRET (constant-time compare). Returns
// the diagnostic payload assembled by release.ReleaseInspector.
//
// Exit-code-equivalent HTTP status mapping (CLI Rule 4):
//   - 200 success
//   - 403 missing or invalid X-Service-Auth (maps to CLI exit 4)
//   - 404 release not found OR LOOPZERO_ADMIN_API not enabled
//         (maps to CLI exit 1)
//   - 500 unexpected internal error

package routes

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/getloopscale/loopzero/api/internal/routes/httpio"
	"net/http"
	"os"

	"github.com/getloopscale/loopzero/api/internal/services/release"

	"github.com/go-chi/chi/v5"
)

// AdminReleaseInspectHandler returns an http.HandlerFunc bound to the
// supplied inspector. The handler is intentionally thin — all logic
// lives in release.ReleaseInspector (DIP).
func AdminReleaseInspectHandler(inspector release.ReleaseInspector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if os.Getenv("LOOPZERO_ADMIN_API") != "true" {
			httpio.WriteCodeErrorJSON(w, http.StatusNotFound, "NOT_FOUND", "Endpoint not available.")
			return
		}

		serviceAuth := r.Header.Get("X-Service-Auth")
		expected := os.Getenv("LOOPZERO_SERVICE_AUTH_SECRET")
		if expected == "" || serviceAuth == "" ||
			subtle.ConstantTimeCompare([]byte(serviceAuth), []byte(expected)) != 1 {
			httpio.WriteCodeErrorJSON(w, http.StatusForbidden, "FORBIDDEN", "Admin endpoint requires X-Service-Auth.")
			return
		}

		releaseID := chi.URLParam(r, "id")
		if releaseID == "" {
			httpio.WriteCodeErrorJSON(w, http.StatusBadRequest, "MISSING_ID", "Release id path parameter is required.")
			return
		}

		inspection, err := inspector.Inspect(r.Context(), releaseID)
		if err != nil {
			var nfe release.ErrReleaseInspectionNotFound
			if errors.As(err, &nfe) {
				httpio.WriteCodeErrorJSON(w, http.StatusNotFound, "RELEASE_NOT_FOUND", "Release does not exist.")
				return
			}
			httpio.WriteCodeErrorJSON(w, http.StatusInternalServerError, "INSPECT_FAILED", "Release inspection failed.")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(inspection)
	}
}
