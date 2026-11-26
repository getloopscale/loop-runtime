// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package bootstraphttp serves the platform bootstrap endpoint.
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
package bootstraphttp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/getloopscale/loop-runtime/api/internal/bootstrap"
	"github.com/getloopscale/loop-runtime/api/internal/httpio"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"github.com/getloopscale/loop-runtime/pkg/naming"

	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// Type aliases for backward compatibility -- canonical definitions are in domain/.
type (
	BootstrapRequest    = ports.BootstrapRequest
	BootstrapStore      = ports.BootstrapStore
	CellPlacementEngine = bootstrap.CellPlacementEngine
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
			httpio.WriteLoopRuntimeError(w, &loopruntimeerrors.LoopRuntimeError{
				Code:    "UNAUTHORIZED",
				Message: "Invalid or missing bootstrap secret",
			})
			return
		}
	}

	var req BootstrapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrInvalidRequest)
		return
	}

	// Validate required fields at the boundary. The user is either named
	// explicitly (admin_user_id, the Kratos identity) or is the legacy GitHub
	// identity, which needs its GitHub id.
	if req.AdminGitHubID == "" && req.AdminUserID == "" {
		httpio.WriteLoopRuntimeError(w, &loopruntimeerrors.LoopRuntimeError{
			Code:    "MISSING_FIELD",
			Message: "admin_user_id or admin_github_id is required",
		})
		return
	}
	if req.AdminLogin == "" {
		httpio.WriteLoopRuntimeError(w, &loopruntimeerrors.LoopRuntimeError{
			Code:    "MISSING_FIELD",
			Message: "admin_login is required",
		})
		return
	}
	if req.TenantSlug == "" {
		httpio.WriteLoopRuntimeError(w, &loopruntimeerrors.LoopRuntimeError{
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
	if errors.Is(assignErr, ports.ErrTenantSlugTaken) {
		httpio.WriteLoopRuntimeError(w, &loopruntimeerrors.LoopRuntimeError{
			Code:    "TENANT_SLUG_TAKEN",
			Message: fmt.Sprintf("tenant_slug %q belongs to another account; choose another slug", req.TenantSlug),
		})
		return
	}
	if assignErr != nil {
		slog.Error("bootstrap failed", "error", assignErr)
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrInternalError)
		return
	}

	httpio.WriteJSON(w, http.StatusOK, BootstrapResponse{
		Status:   "ok",
		UserID:   userID,
		TenantID: tenantID,
		Created:  created,
	})
}
