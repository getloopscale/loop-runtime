// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// PromoteService abstracts the promotion orchestrator for the handler (DIP).
type PromoteService interface {
	Promote(ctx context.Context, req PromoteInput) (*PromoteOutput, error)
}

// PromoteInput is the input for the promote service from the handler.
type PromoteInput struct {
	TenantID      string
	ProjectID     string
	ProjectName   string
	FromEnv       string
	ToEnv         string
	CorrelationID string
	UserID        string
}

// PromoteOutput is the result from the promote service.
type PromoteOutput struct {
	PromotionID    string
	SourceReleaseID string
	NewReleaseID   string
	FromEnv        string
	ToEnv          string
	URL            string
}

// PromoteProjectStore abstracts project lookup for promote (DIP).
type PromoteProjectStore interface {
	GetByName(ctx context.Context, tenantID, name string) (*ProjectResult, error)
}

// promoteRequest is the JSON body for POST /api/v1/projects/{name}/promote.
type promoteRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// promoteResponse is the JSON response for a successful promotion.
type promoteResponse struct {
	PromotionID   string `json:"promotion_id"`
	Status        string `json:"status"`
	SourceRelease string `json:"source_release"`
	NewRelease    string `json:"new_release"`
	FromEnv       string `json:"from"`
	ToEnv         string `json:"to"`
	URL           string `json:"url,omitempty"`
}

// PromoteHandler provides HTTP handlers for promote operations.
type PromoteHandler struct {
	projects   PromoteProjectStore
	service    PromoteService
	baseDomain string
}

// NewPromoteHandler creates a new PromoteHandler.
func NewPromoteHandler(projects PromoteProjectStore, service PromoteService, baseDomain string) *PromoteHandler {
	return &PromoteHandler{
		projects:   projects,
		service:    service,
		baseDomain: baseDomain,
	}
}

// Create handles POST /api/v1/projects/{name}/promote.
func (h *PromoteHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("project name is required"))
		return
	}

	// Parse request body
	var req promoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("invalid request body"))
		return
	}

	if req.From == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("'from' environment is required"))
		return
	}
	if req.To == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("'to' environment is required"))
		return
	}

	// Look up project
	project, err := h.projects.GetByName(ctx, uc.TenantID, name)
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if project == nil {
		writeLoopZeroError(w, loopzeroerrors.NewProjectNotFound(name))
		return
	}

	correlationID := r.Header.Get("X-Correlation-ID")

	// Execute promotion
	result, err := h.service.Promote(ctx, PromoteInput{
		TenantID:      uc.TenantID,
		ProjectID:     name, // Deploy uses project name as project_id in builds/releases
		ProjectName:   name,
		FromEnv:       req.From,
		ToEnv:         req.To,
		CorrelationID: correlationID,
		UserID:        uc.UserID,
	})
	if err != nil {
		var loopzeroErr *loopzeroerrors.LoopZeroError
		if errors.As(err, &loopzeroErr) {
			writeLoopZeroError(w, loopzeroErr)
			return
		}
		// Lock timeout -> 409
		if isPromoteLockTimeout(err) {
			writeJSON(w, http.StatusConflict, map[string]string{
				"code":    "DEPLOY_IN_PROGRESS",
				"message": "Another operation is in progress. Try again in a moment.",
			})
			return
		}
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	resp := promoteResponse{
		PromotionID:   result.PromotionID,
		Status:        "promoting",
		SourceRelease: result.SourceReleaseID,
		NewRelease:    result.NewReleaseID,
		FromEnv:       result.FromEnv,
		ToEnv:         result.ToEnv,
		URL:           result.URL,
	}

	writeJSON(w, http.StatusAccepted, resp)
}

// isPromoteLockTimeout checks if the error is a lock timeout.
func isPromoteLockTimeout(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, loopzeroerrors.ErrLockTimeout) || strings.Contains(err.Error(), "lock timeout")
}
