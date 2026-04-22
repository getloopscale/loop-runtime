// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/domain"
	"github.com/loopzero/loopzero/api/internal/middleware"
)

// PreviewStore is the persistence interface for preview environments.
type PreviewStore interface {
	ListByTenant(tenantID string) ([]domain.Preview, error)
	GetByID(tenantID, previewID string) (*domain.Preview, error)
	RequestDeletion(tenantID, previewID string) error
}

// PreviewHandler handles preview environment API endpoints.
type PreviewHandler struct {
	store  PreviewStore
	logger *slog.Logger
}

// NewPreviewHandler creates a new PreviewHandler.
func NewPreviewHandler(store PreviewStore, logger *slog.Logger) *PreviewHandler {
	return &PreviewHandler{store: store, logger: logger}
}

// List returns all preview environments for the authenticated tenant.
// GET /api/v1/previews
func (h *PreviewHandler) List(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserContext(r.Context())
	if user == nil {
		jsonError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	previews, err := h.store.ListByTenant(user.TenantID)
	if err != nil {
		h.logger.Error("list previews failed", "error", err, "tenant", user.TenantID)
		jsonError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list preview environments")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"previews": previews,
		"total":    len(previews),
	})
}

// Get returns a specific preview environment.
// GET /api/v1/previews/{id}
func (h *PreviewHandler) Get(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserContext(r.Context())
	if user == nil {
		jsonError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	previewID := chi.URLParam(r, "id")
	if previewID == "" {
		jsonError(w, http.StatusBadRequest, "MISSING_ID", "Preview ID required")
		return
	}

	preview, err := h.store.GetByID(user.TenantID, previewID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "NOT_FOUND", "Preview environment not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(preview)
}

// Delete requests deletion of a preview environment.
// DELETE /api/v1/previews/{id}
func (h *PreviewHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUserContext(r.Context())
	if user == nil {
		jsonError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
		return
	}

	previewID := chi.URLParam(r, "id")
	if previewID == "" {
		jsonError(w, http.StatusBadRequest, "MISSING_ID", "Preview ID required")
		return
	}

	if err := h.store.RequestDeletion(user.TenantID, previewID); err != nil {
		h.logger.Error("delete preview failed", "error", err, "preview", previewID)
		jsonError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to request preview deletion")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "deleting",
		"message": "Preview environment deletion requested",
	})
}
