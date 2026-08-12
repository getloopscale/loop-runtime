// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package routes — bootstrap.go
//
// POST /internal/bootstrap
// Internal-only endpoint for platform bootstrap. NOT part of the public API contract.
// Creates initial system records (admin user, system tenant, tenant membership).
// Idempotent: safe to call multiple times (uses ON CONFLICT DO NOTHING in PostgreSQL,
// duplicate detection in memory for tests).
package routes

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"github.com/getloopscale/loopzero/api/internal/routes/httpio"
	"log/slog"
	"net/http"
	"sync"

	"github.com/getloopscale/loopzero/api/internal/domain"
	"github.com/getloopscale/loopzero/pkg/naming"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// Type aliases for backward compatibility -- canonical definitions are in domain/.
type (
	BootstrapRequest    = domain.BootstrapRequest
	BootstrapStore      = domain.BootstrapStore
	CellPlacementEngine = domain.CellPlacementEngine
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

// InMemoryBootstrapStore is a test-only implementation of BootstrapStore.
// It tracks created records in memory to simulate ON CONFLICT DO NOTHING.
type InMemoryBootstrapStore struct {
	mu      sync.Mutex
	users   map[string]bool   // keyed by github_id
	tenants map[string]bool   // keyed by tenant_id
	members map[string]bool   // keyed by "tenant_id:user_id"
	cellIDs map[string]string // keyed by tenant_id → cell_id (ADR-0008)
}

// NewInMemoryBootstrapStore creates a new InMemoryBootstrapStore.
func NewInMemoryBootstrapStore() *InMemoryBootstrapStore {
	return &InMemoryBootstrapStore{
		users:   make(map[string]bool),
		tenants: make(map[string]bool),
		members: make(map[string]bool),
		cellIDs: make(map[string]string),
	}
}

// Bootstrap creates records in memory with idempotency.
func (s *InMemoryBootstrapStore) Bootstrap(_ context.Context, req BootstrapRequest) (string, string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	userID := fmt.Sprintf("github-%s", req.AdminLogin)
	tenantID := fmt.Sprintf("tn-%s", req.TenantSlug)
	memberKey := fmt.Sprintf("%s:%s", tenantID, userID)

	created := false

	if !s.users[req.AdminGitHubID] {
		s.users[req.AdminGitHubID] = true
		created = true
	}
	if !s.tenants[tenantID] {
		s.tenants[tenantID] = true
		s.cellIDs[tenantID] = req.CellID // ADR-0008: track cell assignment
		created = true
	}
	if !s.members[memberKey] {
		s.members[memberKey] = true
		created = true
	}

	return userID, tenantID, created, nil
}

// GetCellID returns the stored cell_id for a tenant (test helper).
func (s *InMemoryBootstrapStore) GetCellID(tenantID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cellIDs[tenantID]
}
