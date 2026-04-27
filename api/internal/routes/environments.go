// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// reservedEnvironmentNames contains names that cannot be used for environments.
var reservedEnvironmentNames = map[string]bool{
	"default":     true,
	"kube-system": true,
	"loopzero-system": true,
	"argocd":      true,
	"monitoring":  true,
}

// EnvironmentEntry is the summary representation of an environment.
type EnvironmentEntry struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// EnvironmentDetail is the detailed representation of an environment.
type EnvironmentDetail struct {
	Name      string           `json:"name"`
	Status    string           `json:"status"`
	CreatedAt string           `json:"created_at,omitempty"`
	Projects  []EnvProjectInfo `json:"projects,omitempty"`
}

// EnvProjectInfo describes a project deployed in an environment.
type EnvProjectInfo struct {
	Name          string `json:"name"`
	ActiveRelease string `json:"active_release,omitempty"`
	URL           string `json:"url,omitempty"`
}

// EnvironmentManager abstracts environment operations on the Tenant CRD.
// In production, this is the TenantCRManager. In tests, a fake.
type EnvironmentManager interface {
	ListEnvironments(ctx context.Context, tenantID string) ([]EnvironmentEntry, error)
	AddEnvironment(ctx context.Context, tenantID, envName string) error
	RemoveEnvironment(ctx context.Context, tenantID, envName string) error
	GetEnvironmentDetail(ctx context.Context, tenantID, envName string) (*EnvironmentDetail, error)
}

// EnvironmentHandler provides HTTP handlers for environment operations.
type EnvironmentHandler struct {
	envMgr EnvironmentManager
}

// NewEnvironmentHandler creates a new EnvironmentHandler.
func NewEnvironmentHandler(envMgr EnvironmentManager) *EnvironmentHandler {
	return &EnvironmentHandler{envMgr: envMgr}
}

// createEnvironmentRequest is the JSON request body for POST /api/v1/environments.
type createEnvironmentRequest struct {
	Name string `json:"name"`
}

// List handles GET /api/v1/environments.
// Returns all environments for the authenticated tenant.
func (h *EnvironmentHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	envs, err := h.envMgr.ListEnvironments(ctx, uc.TenantID)
	if err != nil {
		slog.Error("failed to list environments",
			"tenant_id", uc.TenantID,
			"error", err.Error(),
		)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"environments": envs,
	})
}

// Create handles POST /api/v1/environments.
// Creates a new environment for the authenticated tenant.
func (h *EnvironmentHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	var req createEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInvalidRequest)
		return
	}

	// Validate name format (RFC 1123: lowercase alphanumeric + hyphens, no leading hyphen)
	if !rfc1123.MatchString(req.Name) {
		writeLoopZeroError(w, loopzeroerrors.ErrInvalidName.Wrap("environment name must be lowercase, alphanumeric, and may contain hyphens"))
		return
	}

	// Reject reserved names
	if reservedEnvironmentNames[req.Name] {
		writeLoopZeroError(w, loopzeroerrors.ErrInvalidName.Wrap("environment name is reserved and cannot be used"))
		return
	}

	if err := h.envMgr.AddEnvironment(ctx, uc.TenantID, req.Name); err != nil {
		slog.Error("failed to create environment",
			"tenant_id", uc.TenantID,
			"environment", req.Name,
			"error", err.Error(),
		)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"name":   req.Name,
		"status": "active",
	})
}

// Get handles GET /api/v1/environments/{name}.
// Returns detail for a single environment.
func (h *EnvironmentHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("environment name is required"))
		return
	}

	detail, err := h.envMgr.GetEnvironmentDetail(ctx, uc.TenantID, name)
	if err != nil {
		slog.Error("failed to get environment detail",
			"tenant_id", uc.TenantID,
			"environment", name,
			"error", err.Error(),
		)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if detail == nil {
		writeLoopZeroError(w, loopzeroerrors.NewEnvironmentNotFound(name))
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

// Delete handles DELETE /api/v1/environments/{name}.
// Removes an environment. Refuses to delete "production".
func (h *EnvironmentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("environment name is required"))
		return
	}

	// Protect production environment from deletion
	if name == "production" {
		writeLoopZeroError(w, loopzeroerrors.ErrForbidden.Wrap("the production environment cannot be deleted"))
		return
	}

	if err := h.envMgr.RemoveEnvironment(ctx, uc.TenantID, name); err != nil {
		// AUDIT-12: map "not found" to 404 instead of silent success.
		// EnvironmentManager returns a sentinel that we match by message suffix
		// — keeping routes package free of direct k8s import dependencies.
		if strings.Contains(err.Error(), "environment not found in tenant CR") {
			writeLoopZeroError(w, loopzeroerrors.NewEnvironmentNotFound(name))
			return
		}
		slog.Error("failed to delete environment",
			"tenant_id", uc.TenantID,
			"environment", name,
			"error", err.Error(),
		)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"deleted": name,
	})
}

// StubEnvironmentManager is a stub implementation of EnvironmentManager
// that returns hardcoded data. Used in dev mode while the real K8s
// integration (TenantCRManager) is done later.
type StubEnvironmentManager struct{}

// ListEnvironments returns a hardcoded list with a single production environment.
func (s *StubEnvironmentManager) ListEnvironments(_ context.Context, _ string) ([]EnvironmentEntry, error) {
	return []EnvironmentEntry{
		{Name: "production", Status: "active"},
	}, nil
}

// AddEnvironment is a no-op stub.
func (s *StubEnvironmentManager) AddEnvironment(_ context.Context, _, _ string) error {
	return nil
}

// RemoveEnvironment is a no-op stub.
func (s *StubEnvironmentManager) RemoveEnvironment(_ context.Context, _, _ string) error {
	return nil
}

// GetEnvironmentDetail returns a hardcoded detail for production, nil otherwise.
func (s *StubEnvironmentManager) GetEnvironmentDetail(_ context.Context, _ string, envName string) (*EnvironmentDetail, error) {
	if envName == "production" {
		return &EnvironmentDetail{
			Name:     "production",
			Status:   "active",
			Projects: []EnvProjectInfo{},
		}, nil
	}
	return nil, nil
}
