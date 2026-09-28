// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// admin_evict.go implements T-02 bulk tenant eviction endpoint from
// 50k-immediate-focus.
//
// Gated behind LOOPZERO_ADMIN_API=true. Requires X-Service-Auth header
// (not user auth) since eviction is an operator primitive, not a tenant
// capability.
//
// The endpoint is intentionally minimal: it enqueues eviction work and
// returns a job ID. Actual eviction is done by placement.TenantEvictor
// (separate file) which handles FK ordering + finalizer removal.

// Package placementhttp serves the placement feature's operator endpoints under
// /api/v1/admin: evicting a tenant from a cell and pinning one to a specific
// cell. Until api/internal was organized by feature (2026-09-25) they shared the
// `admin` family with the release inspection, which now lives in the release
// feature.
//
// History of the admin family: fifteenth family to leave the flat `routes` package (B-080, slice-35), and the
// first with three production files — the three admin_* files are one family by
// audience rather than by subject: every route here is X-Service-Auth only, with
// no user context, which is why the router registers them as a sibling of the
// webhook rather than behind the user-auth middleware.
//
// It is also the family that pulled `WriteCodeErrorJSON` into the `httpio` leaf
// two slices ago: two of these three files reached a response writer that lived
// in the audit family, and the extraction of THAT family is what surfaced it.
package placementhttp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/api/internal/httpio"
)

// AdminEvictInput is the body for POST /api/v1/admin/tenants/evict.
//
// Filter semantics:
//   - Pattern (SQL LIKE) matches against tenants.id column. Examples:
//     "tn-lt-%" (all loadtest), "tn-lt-l3-%" (L3 only), exact "tn-abc-123".
//   - IDs list explicit tenant IDs to evict.
//   - At least one MUST be set. Both can be used (union).
type AdminEvictInput struct {
	Pattern string   `json:"pattern,omitempty"`
	IDs     []string `json:"ids,omitempty"`
	// DryRun reports matched tenants without evicting. Safe for preflight.
	DryRun bool `json:"dry_run,omitempty"`
}

// AdminEvictResponse is returned on success (202 Accepted).
type AdminEvictResponse struct {
	JobID    string   `json:"job_id"`
	Queued   int      `json:"queued"`
	Matched  []string `json:"matched,omitempty"` // only in dry_run mode
	Accepted bool     `json:"accepted"`
}

// TenantEvictor is the service interface for bulk tenant teardown.
// Implementation lives in services/tenant_evictor.go (scaffold).
type TenantEvictor interface {
	// MatchTenants resolves a pattern + IDs into a concrete list.
	MatchTenants(ctx context.Context, pattern string, ids []string) ([]string, error)
	// EnqueueEviction submits the tenant IDs for async teardown.
	// Returns job_id for progress tracking.
	EnqueueEviction(ctx context.Context, tenantIDs []string) (string, error)
}

// AdminEvictHandler implements POST /api/v1/admin/tenants/evict.
//
// Security:
//   - LOOPZERO_ADMIN_API env must be "true" (feature flag off by default)
//   - X-Service-Auth check is enforced by Authenticator.RequireAdminService
//     at the router layer (qa-sprint2-cleanup-2026-06-24 T1.2 migration —
//     closes loopzero-cloud#16). The middleware accepts ONLY the canonical
//     LOOPZERO_ADMIN_SERVICE_AUTH_SECRET (+ _PREVIOUS rotation sibling); the
//     legacy LOOPZERO_SERVICE_AUTH_SECRET that the cloud /engine/* proxy adds
//     to every user-JWT request is NOT honored for admin authorization.
//
// The handler validates input, resolves matches via evictor, and enqueues.
// Actual FK-ordered DELETE + finalizer cleanup is in services layer.
func AdminEvictHandler(evictor TenantEvictor, apiEnabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !apiEnabled { // LOOPZERO_ADMIN_API, read once at boot
			httpio.WriteCodeErrorJSON(w, http.StatusNotFound, "NOT_FOUND", "Endpoint not available.")
			return
		}

		var input AdminEvictInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			httpio.WriteCodeErrorJSON(w, http.StatusBadRequest, "INVALID_BODY", "Request body must be JSON matching AdminEvictInput.")
			return
		}

		if strings.TrimSpace(input.Pattern) == "" && len(input.IDs) == 0 {
			httpio.WriteCodeErrorJSON(w, http.StatusBadRequest, "MISSING_FILTER", "Either 'pattern' or 'ids' must be set.")
			return
		}

		// Require explicit acknowledgement for full-match patterns.
		if input.Pattern == "%" || input.Pattern == "tn-%" {
			httpio.WriteCodeErrorJSON(w, http.StatusBadRequest, "PATTERN_TOO_BROAD",
				"Pattern '%' or 'tn-%' requires explicit enumeration — too dangerous as free-text.")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		matched, err := evictor.MatchTenants(ctx, input.Pattern, input.IDs)
		if err != nil {
			// Same split as the enqueue failure below (CWE-209): the upstream
			// error goes to the log, the caller gets a message safe to show.
			slog.ErrorContext(ctx, "eviction tenant match failed", "err", err, "pattern", input.Pattern, "ids_count", len(input.IDs))
			httpio.WriteCodeErrorJSON(w, http.StatusInternalServerError, "MATCH_FAILED",
				"Failed to resolve tenants. Please retry.")
			return
		}

		if input.DryRun {
			httpio.WriteJSON(w, http.StatusOK, AdminEvictResponse{
				JobID:    "dry-run",
				Queued:   len(matched),
				Matched:  matched,
				Accepted: false,
			})
			return
		}

		jobID, err := evictor.EnqueueEviction(ctx, matched)
		if err != nil {
			// T2.1 of plan qa-sprint1-cleanup-2026-06-24 v1.2 — closes #20
			// (CWE-209 info-disclosure). Split client message + server log:
			// the prior `fmt.Sprintf("%v", err)` leaked k8s ServiceAccount,
			// namespace, and ArgoCD APIgroup verbatim to the authenticated
			// service-auth caller. Per ADR D6 (split over redact-regex), the
			// detailed err goes to slog (Loki/audit ingest); the client gets
			// a generic message that is unconditionally safe regardless of
			// the err shape coming from k8s/ArgoCD upstream.
			slog.ErrorContext(ctx, "eviction enqueue failed",
				"err", err,
				"tenants_matched_count", len(matched),
			)
			httpio.WriteCodeErrorJSON(w, http.StatusInternalServerError, "ENQUEUE_FAILED",
				"Eviction queue temporarily unavailable. Please retry.")
			return
		}

		httpio.WriteJSON(w, http.StatusAccepted, AdminEvictResponse{
			JobID:    jobID,
			Queued:   len(matched),
			Accepted: true,
		})
	}
}

// jsonError lives in audit.go; the response writers moved to httpio
// (slice-5) so that no family depends on another to render a response.
