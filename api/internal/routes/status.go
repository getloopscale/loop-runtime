// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// StatusBuildStore abstracts build retrieval for the status handler (DIP).
type StatusBuildStore interface {
	ListByProject(ctx context.Context, tenantID, projectID string, limit, offset int) ([]StatusBuild, error)
}

// StatusBuild is the build representation used by the status handler.
// Decoupled from db.Build to respect DIP at the route boundary.
type StatusBuild struct {
	ID              string
	Environment     string
	Status          string
	CommitSHA       string
	CreatedAt       time.Time
	FinishedAt      *time.Time
	DurationSeconds *int
	ErrorMessage    string
}

// StatusReleaseStore abstracts active release retrieval for the status handler (DIP).
type StatusReleaseStore interface {
	GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*StatusRelease, error)
}

// StatusRelease is the release representation used by the status handler.
type StatusRelease struct {
	ID        string
	Status    string
	GitCommit string
	CreatedAt time.Time
}

// StatusProjectStore abstracts project lookup for the status handler (DIP).
type StatusProjectStore interface {
	GetByName(ctx context.Context, tenantID, name string) (*ProjectResult, error)
}

// StatusHandler provides HTTP handlers for project status operations.
type StatusHandler struct {
	projects   StatusProjectStore
	builds     StatusBuildStore
	releases   StatusReleaseStore
	baseDomain string
}

// NewStatusHandler creates a new StatusHandler.
func NewStatusHandler(projects StatusProjectStore, builds StatusBuildStore, releases StatusReleaseStore, baseDomain string) *StatusHandler {
	return &StatusHandler{
		projects:   projects,
		builds:     builds,
		releases:   releases,
		baseDomain: baseDomain,
	}
}

// lastBuildResponse is the JSON representation of the last build.
type lastBuildResponse struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	CommitSHA       string `json:"commit_sha"`
	CreatedAt       string `json:"created_at"`
	FinishedAt      string `json:"finished_at"`
	DurationSeconds int    `json:"duration_seconds"`
	Error           string `json:"error"`
}

// activeReleaseResponse is the JSON representation of the active release.
type activeReleaseResponse struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	GitCommit string `json:"git_commit"`
	CreatedAt string `json:"created_at"`
}

// statusResponse is the JSON response for GET /api/v1/projects/{name}/status.
type statusResponse struct {
	Project       string                `json:"project"`
	Environment   string                `json:"environment"`
	LastBuild     *lastBuildResponse    `json:"last_build"`
	ActiveRelease *activeReleaseResponse `json:"active_release"`
	SyncStatus    string                `json:"sync_status"`
	URL           string                `json:"url"`
}

// Get handles GET /api/v1/projects/{name}/status?env={env}.
func (h *StatusHandler) Get(w http.ResponseWriter, r *http.Request) {
	uc := middleware.GetUserContext(r.Context())
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("project name is required"))
		return
	}

	env := r.URL.Query().Get("env")
	if env == "" {
		env = "production"
	}

	// Look up project by name to get projectID
	project, err := h.projects.GetByName(r.Context(), uc.TenantID, name)
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if project == nil {
		writeLoopZeroError(w, loopzeroerrors.NewProjectNotFound(name))
		return
	}

	resp := statusResponse{
		Project:     name,
		Environment: env,
		SyncStatus:  "unknown",
	}

	// Get last build for this project+env
	builds, err := h.builds.ListByProject(r.Context(), uc.TenantID, project.ID, 50, 0)
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	// Filter by environment and take the first match (already sorted by created_at DESC)
	for i := range builds {
		if builds[i].Environment == env {
			b := builds[i]
			lb := &lastBuildResponse{
				ID:        b.ID,
				Status:    b.Status,
				CommitSHA: b.CommitSHA,
				CreatedAt: b.CreatedAt.UTC().Format(time.RFC3339),
				Error:     b.ErrorMessage,
			}
			if b.FinishedAt != nil {
				lb.FinishedAt = b.FinishedAt.UTC().Format(time.RFC3339)
			}
			if b.DurationSeconds != nil {
				lb.DurationSeconds = *b.DurationSeconds
			}
			resp.LastBuild = lb
			break
		}
	}

	// Get active release for this env
	release, err := h.releases.GetActiveByEnv(r.Context(), uc.TenantID, project.ID, env)
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if release != nil {
		resp.ActiveRelease = &activeReleaseResponse{
			ID:        release.ID,
			Status:    release.Status,
			GitCommit: release.GitCommit,
			CreatedAt: release.CreatedAt.UTC().Format(time.RFC3339),
		}
		resp.SyncStatus = "synced"
	}

	// Build URL
	if h.baseDomain != "" {
		resp.URL = fmt.Sprintf("https://%s-%s.%s", name, env, h.baseDomain)
	}

	writeJSON(w, http.StatusOK, resp)
}
