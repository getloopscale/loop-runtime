// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-30-INFRA-NORMALIZE / Phase 5 T5.5 — admin endpoint to pin
// a tenant to a specific cell. Closes plan task T5.5 (CLI `--cell` flag
// path) and MEET-INFRA-NORMALIZE-T3-multi-cell condition C8 (DOKS-side
// authz: pinning is operator-only, not tenant-callable).
//
// Why an admin endpoint, not a tenant-self-service flag?
//   • T-1.3 Security veto C8: cell pinning must require operator authz
//     (X-Service-Auth header, NOT user-bearer-token). Self-service
//     bypass would let any tenant migrate its own workload across
//     cells, breaking placement layer's capacity gate semantics.
//   • The plan's "loopzero deploy --cell <id>" wording was loose; the
//     SHIPPING shape is "loopzero admin tenants pin --cell <id> --tenant
//     <id>". Wire-shape change captured here for the CLI to consume.
//
// Gated behind LOOPZERO_ADMIN_API=true (same flag as admin_evict). Requires
// X-Service-Auth header. Fails 403 without either.

package routes

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/getloopscale/loopzero/api/internal/services"
)

// AdminPinCellInput is the body for POST /api/v1/admin/tenants/pin.
//
// Both fields required:
//   • TenantID — must exist in tenants table.
//   • CellID   — must match cells table (gated by FK semantics in
//                CellPinner.Pin); must be in 'active' status (Pin
//                rejects pinning to draining/maintenance/retired).
type AdminPinCellInput struct {
	TenantID string `json:"tenant_id"`
	CellID   string `json:"cell_id"`
}

// AdminPinCellResponse on success (200 OK).
//
// Reports the previous cell so operator can rollback via a second
// call: `pin --tenant <id> --cell <previous>`.
type AdminPinCellResponse struct {
	TenantID    string `json:"tenant_id"`
	PreviousCell string `json:"previous_cell"`
	NewCell     string `json:"new_cell"`
	Updated     bool   `json:"updated"`
}

// CellPinner is the service interface for pinning a tenant to a cell.
// Implementation lives in services/cell_pinner.go (scaffold next).
//
// Errors returned:
//   • ErrCellNotFound      — cell_id does not exist in cells table.
//   • ErrCellNotPinnable   — cells.status ∈ {draining, maintenance,
//                            retired}; only 'active' accepts pin.
//   • ErrCellAtCapacity    — cells.capacity_tenants_max reached.
//   • ErrTenantNotFound    — tenant_id does not exist.
type CellPinner interface {
	Pin(ctx context.Context, tenantID, cellID string) (previousCell string, err error)
}

// validCellIDPattern mirrors cells.cell_id CHECK + ClusterCredential
// kubebuilder pattern. Reject early so the handler returns 400 instead
// of letting bad input reach the DB layer.
var validCellIDPattern = regexp.MustCompile(`^cell-[a-z0-9-]+$`)

// AdminPinCellHandler implements POST /api/v1/admin/tenants/pin.
func AdminPinCellHandler(pinner CellPinner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Feature flag gate (matches admin_evict semantics).
		if os.Getenv("LOOPZERO_ADMIN_API") != "true" {
			http.NotFound(w, r)
			return
		}

		// Service-auth gate.
		auth := strings.TrimSpace(r.Header.Get("X-Service-Auth"))
		expected := os.Getenv("LOOPZERO_ADMIN_SERVICE_AUTH_SECRET")
		if expected == "" || subtle.ConstantTimeCompare([]byte(auth), []byte(expected)) != 1 {
			http.Error(w, `{"code":"forbidden","message":"admin endpoint requires X-Service-Auth"}`, http.StatusForbidden)
			return
		}

		var in AdminPinCellInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, `{"code":"bad_request","message":"invalid JSON body"}`, http.StatusBadRequest)
			return
		}

		// Validate tenant_id (non-empty; format validation lives in DB layer).
		if strings.TrimSpace(in.TenantID) == "" {
			http.Error(w, `{"code":"bad_request","message":"tenant_id required"}`, http.StatusBadRequest)
			return
		}

		// Validate cell_id (matches CHECK constraint).
		if !validCellIDPattern.MatchString(in.CellID) {
			http.Error(w, `{"code":"bad_request","message":"cell_id must match ^cell-[a-z0-9-]+$"}`, http.StatusBadRequest)
			return
		}

		// Pinner is required; if nil, the feature is wired but service
		// not yet provided (CI gate). Fail closed.
		if pinner == nil {
			http.Error(w, `{"code":"unavailable","message":"cell pinner not configured"}`, http.StatusServiceUnavailable)
			return
		}

		previous, err := pinner.Pin(r.Context(), in.TenantID, in.CellID)
		if err != nil {
			// T6.1 of deep-review-remediation-plan (find_code_001 / CODE-001 HIGH).
			// errors.Is against services sentinels — became safe to import
			// services/ after T1.3 closed the routes/→domain/ cycle (find_arch_001).
			status := http.StatusInternalServerError
			code := "internal"
			switch {
			case errors.Is(err, services.ErrTenantNotFound):
				status = http.StatusNotFound
				code = "tenant_not_found"
			case errors.Is(err, services.ErrCellNotFound):
				status = http.StatusNotFound
				code = "cell_not_found"
			case errors.Is(err, services.ErrCellNotPinnable):
				status = http.StatusConflict
				code = "cell_not_pinnable"
			case errors.Is(err, services.ErrCellAtCapacity):
				status = http.StatusConflict
				code = "cell_at_capacity"
			}
			http.Error(w, `{"code":"`+code+`","message":"`+err.Error()+`"}`, status)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(AdminPinCellResponse{
			TenantID:     in.TenantID,
			PreviousCell: previous,
			NewCell:      in.CellID,
			Updated:      previous != in.CellID,
		})
	}
}
