// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// admin_release_inspect.go implements T4.4 of plan
// 2026-05-28-meet-001-engineering-closure-plan.md.
//
// GET /api/v1/admin/releases/{id}/inspect
//
// Gated behind LOOPZERO_ADMIN_API=true. Service authentication is NOT done
// here: the router mounts this handler behind Authenticator.RequireAdminService,
// the one admin gate (canonical LOOPZERO_ADMIN_SERVICE_AUTH_SECRET only).
// Returns the diagnostic payload assembled by release.ReleaseInspector.
//
// Exit-code-equivalent HTTP status mapping (CLI Rule 4):
//   - 200 success
//   - 403 missing or invalid X-Service-Auth (maps to CLI exit 4)
//   - 404 release not found OR LOOPZERO_ADMIN_API not enabled
//         (maps to CLI exit 1)
//   - 500 unexpected internal error

// Package admin serves the release-inspection operator endpoint; eviction and
// cell pinning moved to the placement feature (api/internal organized by
// feature, 2026-09-25).
package admin

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/getloopscale/loopzero/api/internal/httpio"

	"github.com/getloopscale/loopzero/api/internal/services/release"

	"github.com/go-chi/chi/v5"
)

// AdminReleaseInspectHandler returns an http.HandlerFunc bound to the
// supplied inspector. The handler is intentionally thin — all logic
// lives in release.ReleaseInspector (DIP).
func AdminReleaseInspectHandler(inspector release.ReleaseInspector, apiEnabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !apiEnabled { // LOOPZERO_ADMIN_API, read once at boot
			httpio.WriteCodeErrorJSON(w, http.StatusNotFound, "NOT_FOUND", "Endpoint not available.")
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
