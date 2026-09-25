// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package bootstrap serves the platform bootstrap endpoint.
//
// POST /internal/bootstrap
// Internal-only endpoint for platform bootstrap. NOT part of the public API contract.
// Creates initial system records (admin user, system tenant, tenant membership).
// Idempotent: safe to call multiple times (uses ON CONFLICT DO NOTHING in PostgreSQL,
// duplicate detection in memory for tests).
// Twelfth family to leave the flat `routes` package (B-080, slice-33): the first
// call a new tenant makes,
// which assigns it to a cell and creates the records the rest of the platform
// reads.
//
// Twelfth family to leave the flat `routes` package (B-080, slice-33).
//
// Placement is sticky and one-time (ADR-0008), so this handler is the only path
// that decides a tenant's cell — everything downstream is lookup. The types it
// names for that decision, `BootstrapStore` and `CellPlacementEngine`, are ALIASES
// re-exported from `internal/domain`; consumers now reference the real types, so
// the apparent coupling to this family never existed. Third family in a row where
// the compiler surfaced an alias the fence could not see, because the fence
// measures imports and an alias hides behind the package that declares it.
package bootstrap

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/getloopscale/loopzero/api/internal/httpio"

	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/pkg/naming"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// Type aliases for backward compatibility -- canonical definitions are in domain/.
type (
	BootstrapRequest    = ports.BootstrapRequest
	BootstrapStore      = ports.BootstrapStore
	CellPlacementEngine = ports.CellPlacementEngine
)

// BootstrapResponse is the JSON response for POST /internal/bootstrap.
type BootstrapResponse struct {
	Status   string `json:"status"`
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
	Created  bool   `json:"created"`
}

// BootstrapHandler handles POST /internal/bootstrap.
type BootstrapHandler struct {
	store     BootstrapStore
	secret    string              // SEC-003: shared secret for authentication
	placement CellPlacementEngine // ADR-0008: Cell assignment
}

// NewBootstrapHandler creates a new BootstrapHandler.
// The secret parameter is the shared secret for authenticating bootstrap requests.
// When non-empty, requests must include Authorization: Bearer <secret>.
func NewBootstrapHandler(store BootstrapStore, secret string, placement CellPlacementEngine) *BootstrapHandler {
	return &BootstrapHandler{store: store, secret: secret, placement: placement}
}

// Bootstrap handles POST /internal/bootstrap.
// SEC-003: Validates shared secret before processing. Without this, any pod in the
// cluster could create admin tenants.
func (h *BootstrapHandler) Bootstrap(w http.ResponseWriter, r *http.Request) {
	// SEC-003: Validate bootstrap secret when configured.
	// B3: Use constant-time comparison to prevent timing attacks on secret values.
	if h.secret != "" {
		auth := r.Header.Get("Authorization")
		expected := "Bearer " + h.secret
		if subtle.ConstantTimeCompare([]byte(auth), []byte(expected)) != 1 {
			httpio.WriteLoopZeroError(w, &loopzeroerrors.LoopZeroError{
				Code:    "UNAUTHORIZED",
				Message: "Invalid or missing bootstrap secret",
			})
			return
		}
	}

	var req BootstrapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInvalidRequest)
		return
	}

	// Validate required fields at the boundary.
	if req.AdminGitHubID == "" {
		httpio.WriteLoopZeroError(w, &loopzeroerrors.LoopZeroError{
			Code:    "MISSING_FIELD",
			Message: "admin_github_id is required",
		})
		return
	}
	if req.AdminLogin == "" {
		httpio.WriteLoopZeroError(w, &loopzeroerrors.LoopZeroError{
			Code:    "MISSING_FIELD",
			Message: "admin_login is required",
		})
		return
	}
	if req.TenantSlug == "" {
		httpio.WriteLoopZeroError(w, &loopzeroerrors.LoopZeroError{
			Code:    "MISSING_FIELD",
			Message: "tenant_slug is required",
		})
		return
	}

	// Default plan to starter if not provided.
	if req.Plan == "" {
		req.Plan = "starter"
	}

	// Sanitize the tenant slug to ensure RFC 1123 compliance.
	req.TenantSlug = naming.Sanitize(req.TenantSlug)

	// MEET-053 D-01: AssignAndExecute runs the cell pick AND the bootstrap
	// INSERT inside the same serialized transaction. This fixes the TOCTOU
	// race where concurrent tenant creations could both read the same cell
	// counts and both pick the same cell.
	var userID, tenantID string
	var created bool
	assignErr := h.placement.AssignAndExecute(r.Context(), func(txCtx context.Context, cellID string) error {
		req.CellID = cellID
		var err error
		userID, tenantID, created, err = h.store.Bootstrap(txCtx, req)
		return err
	})
	if assignErr != nil {
		slog.Error("bootstrap failed", "error", assignErr)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	httpio.WriteJSON(w, http.StatusOK, BootstrapResponse{
		Status:   "ok",
		UserID:   userID,
		TenantID: tenantID,
		Created:  created,
	})
}
