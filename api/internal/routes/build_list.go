// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// BuildListItem is the route-level representation of a build for listing.
// Avoids importing db package (prevents import cycle with db → routes).
type BuildListItem struct {
	ID              string
	ProjectID       string
	Environment     string
	Status          string
	CommitSHA       string
	CreatedBy       string
	CreatedAt       time.Time
	StartedAt       *time.Time
	FinishedAt      *time.Time
	DurationSeconds *int
	ErrorMessage    string
}

// BuildListStore abstracts build list retrieval (DIP).
type BuildListStore interface {
	ListByProjectAndEnv(ctx context.Context, tenantID, projectID, env string, limit, offset int) ([]BuildListItem, int, error)
}

// BuildListHandler provides HTTP handlers for listing builds.
type BuildListHandler struct {
	builds BuildListStore
}

// NewBuildListHandler creates a new BuildListHandler.
func NewBuildListHandler(builds BuildListStore) *BuildListHandler {
	return &BuildListHandler{builds: builds}
}

// buildListResponse is the JSON response for GET /api/v1/projects/{name}/builds.
type buildListResponse struct {
	Builds []BuildStatusResponse `json:"builds"`
	Total  int                   `json:"total"`
}

// List handles GET /api/v1/projects/{name}/builds.
// Query params: env (optional), limit (default 20, max 50), offset (default 0).
func (h *BuildListHandler) List(w http.ResponseWriter, r *http.Request) {
	uc := middleware.GetUserContext(r.Context())
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	name := chi.URLParam(r, "name")

	env := r.URL.Query().Get("env")
	limit := parseIntParam(r.URL.Query().Get("limit"), 20)
	offset := parseIntParam(r.URL.Query().Get("offset"), 0)

	builds, total, err := h.builds.ListByProjectAndEnv(r.Context(), uc.TenantID, name, env, limit, offset)
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	resp := buildListResponse{
		Builds: make([]BuildStatusResponse, 0, len(builds)),
		Total:  total,
	}

	for _, b := range builds {
		item := BuildStatusResponse{
			BuildID:         b.ID,
			ProjectID:       b.ProjectID,
			Environment:     b.Environment,
			Status:          b.Status,
			CommitSHA:       b.CommitSHA,
			CreatedBy:       b.CreatedBy,
			CreatedAt:       b.CreatedAt.UTC().Format(time.RFC3339),
			DurationSeconds: b.DurationSeconds,
			ErrorMessage:    b.ErrorMessage,
		}
		if b.StartedAt != nil {
			item.StartedAt = b.StartedAt.UTC().Format(time.RFC3339)
		}
		if b.FinishedAt != nil {
			item.FinishedAt = b.FinishedAt.UTC().Format(time.RFC3339)
		}
		resp.Builds = append(resp.Builds, item)
	}

	writeJSON(w, http.StatusOK, resp)
}

// parseIntParam parses a string to int, returning defaultVal on failure or empty string.
func parseIntParam(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return v
}
