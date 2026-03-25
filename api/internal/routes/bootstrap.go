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
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/loopzero/loopzero/pkg/naming"

	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// BootstrapRequest is the JSON body for POST /internal/bootstrap.
type BootstrapRequest struct {
	AdminGitHubID    string `json:"admin_github_id"`
	AdminLogin       string `json:"admin_login"`
	TenantSlug       string `json:"tenant_slug"`
	TenantDisplayName string `json:"tenant_display_name"`
	Plan             string `json:"plan"`
}

// BootstrapResponse is the JSON response for POST /internal/bootstrap.
type BootstrapResponse struct {
	Status   string `json:"status"`
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
	Created  bool   `json:"created"`
}

// BootstrapStore abstracts the database operations for bootstrap.
// Implemented by PostgresBootstrapStore (production) and InMemoryBootstrapStore (tests).
type BootstrapStore interface {
	// Bootstrap creates the admin user, tenant, and membership records.
	// Returns (userID, tenantID, created, error).
	// created is true if any record was actually inserted (not already existing).
	// Must be idempotent — duplicate calls with the same data must not fail.
	Bootstrap(ctx context.Context, req BootstrapRequest) (userID, tenantID string, created bool, err error)
}

// BootstrapHandler handles POST /internal/bootstrap.
type BootstrapHandler struct {
	store BootstrapStore
}

// NewBootstrapHandler creates a new BootstrapHandler.
func NewBootstrapHandler(store BootstrapStore) *BootstrapHandler {
	return &BootstrapHandler{store: store}
}

// Bootstrap handles POST /internal/bootstrap.
func (h *BootstrapHandler) Bootstrap(w http.ResponseWriter, r *http.Request) {
	var req BootstrapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInvalidRequest)
		return
	}

	// Validate required fields at the boundary.
	if req.AdminGitHubID == "" {
		writeLoopZeroError(w, &loopzeroerrors.LoopZeroError{
			Code:       "MISSING_FIELD",
			Message:    "admin_github_id is required",
			HTTPStatus: http.StatusBadRequest,
		})
		return
	}
	if req.AdminLogin == "" {
		writeLoopZeroError(w, &loopzeroerrors.LoopZeroError{
			Code:       "MISSING_FIELD",
			Message:    "admin_login is required",
			HTTPStatus: http.StatusBadRequest,
		})
		return
	}
	if req.TenantSlug == "" {
		writeLoopZeroError(w, &loopzeroerrors.LoopZeroError{
			Code:       "MISSING_FIELD",
			Message:    "tenant_slug is required",
			HTTPStatus: http.StatusBadRequest,
		})
		return
	}

	// Default plan to starter if not provided.
	if req.Plan == "" {
		req.Plan = "starter"
	}

	// Sanitize the tenant slug to ensure RFC 1123 compliance.
	req.TenantSlug = naming.Sanitize(req.TenantSlug)

	userID, tenantID, created, err := h.store.Bootstrap(r.Context(), req)
	if err != nil {
		slog.Error("bootstrap failed", "error", err)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	writeJSON(w, http.StatusOK, BootstrapResponse{
		Status:   "ok",
		UserID:   userID,
		TenantID: tenantID,
		Created:  created,
	})
}

// InMemoryBootstrapStore is a test-only implementation of BootstrapStore.
// It tracks created records in memory to simulate ON CONFLICT DO NOTHING.
type InMemoryBootstrapStore struct {
	mu       sync.Mutex
	users    map[string]bool // keyed by github_id
	tenants  map[string]bool // keyed by tenant_id
	members  map[string]bool // keyed by "tenant_id:user_id"
}

// NewInMemoryBootstrapStore creates a new InMemoryBootstrapStore.
func NewInMemoryBootstrapStore() *InMemoryBootstrapStore {
	return &InMemoryBootstrapStore{
		users:   make(map[string]bool),
		tenants: make(map[string]bool),
		members: make(map[string]bool),
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
		created = true
	}
	if !s.members[memberKey] {
		s.members[memberKey] = true
		created = true
	}

	return userID, tenantID, created, nil
}
