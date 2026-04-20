// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package routes — admin_evict.go implements T-02 bulk tenant eviction
// endpoint from docs/plans/50k-immediate-focus.md.
//
// Gated behind LOOPZERO_ADMIN_API=true. Requires X-Service-Auth header
// (not user auth) since eviction is an operator primitive, not a tenant
// capability.
//
// The endpoint is intentionally minimal: it enqueues eviction work and
// returns a job ID. Actual eviction is done by services.TenantEvictor
// (separate file) which handles FK ordering + finalizer removal.

package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
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
//   - X-Service-Auth header must match LOOPZERO_SERVICE_AUTH_SECRET
//   - Any failure returns 403 without leaking whether the flag is on
//
// The handler validates input, resolves matches via evictor, and enqueues.
// Actual FK-ordered DELETE + finalizer cleanup is in services layer.
func AdminEvictHandler(evictor TenantEvictor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if os.Getenv("LOOPZERO_ADMIN_API") != "true" {
			jsonError(w, http.StatusNotFound, "NOT_FOUND", "Endpoint not available.")
			return
		}

		serviceAuth := r.Header.Get("X-Service-Auth")
		expected := os.Getenv("LOOPZERO_SERVICE_AUTH_SECRET")
		if expected == "" || serviceAuth == "" || serviceAuth != expected {
			jsonError(w, http.StatusForbidden, "FORBIDDEN", "Admin endpoint requires X-Service-Auth.")
			return
		}

		var input AdminEvictInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			jsonError(w, http.StatusBadRequest, "INVALID_BODY", "Request body must be JSON matching AdminEvictInput.")
			return
		}

		if strings.TrimSpace(input.Pattern) == "" && len(input.IDs) == 0 {
			jsonError(w, http.StatusBadRequest, "MISSING_FILTER", "Either 'pattern' or 'ids' must be set.")
			return
		}

		// Require explicit acknowledgement for full-match patterns.
		if input.Pattern == "%" || input.Pattern == "tn-%" {
			jsonError(w, http.StatusBadRequest, "PATTERN_TOO_BROAD",
				"Pattern '%' or 'tn-%' requires explicit enumeration — too dangerous as free-text.")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		matched, err := evictor.MatchTenants(ctx, input.Pattern, input.IDs)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "MATCH_FAILED",
				fmt.Sprintf("Failed to resolve tenants: %v", err))
			return
		}

		if input.DryRun {
			writeJSON(w, http.StatusOK, AdminEvictResponse{
				JobID:    "dry-run",
				Queued:   len(matched),
				Matched:  matched,
				Accepted: false,
			})
			return
		}

		jobID := uuid.NewString()
		if _, err := evictor.EnqueueEviction(ctx, matched); err != nil {
			jsonError(w, http.StatusInternalServerError, "ENQUEUE_FAILED",
				fmt.Sprintf("Failed to enqueue eviction: %v", err))
			return
		}

		writeJSON(w, http.StatusAccepted, AdminEvictResponse{
			JobID:    jobID,
			Queued:   len(matched),
			Accepted: true,
		})
	}
}

// writeJSON/jsonError already exist in the package — see projects.go.
