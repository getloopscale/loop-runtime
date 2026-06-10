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

// ProjectRepository represents a project↔repo link (DIP — avoids db import).
type ProjectRepository struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	ProjectName    string    `json:"project_name"`
	Provider       string    `json:"provider"`
	RepoFullName   string    `json:"repo_full_name"`
	InstallationID int64     `json:"installation_id"`
	DefaultBranch  string    `json:"default_branch"`
	AutoDeploy     bool      `json:"auto_deploy"`
	CreatedAt      time.Time `json:"created_at"`
}

// GitHubInstallationInfo is a minimal struct for saving installations (DIP).
type GitHubInstallationInfo struct {
	ID           int64
	AccountLogin string
	AccountType  string
	TenantID     *string
}

// RepositoryStore abstracts project↔repository persistence (DIP).
type RepositoryStore interface {
	LinkRepository(ctx context.Context, tenantID, projectName, repoFullName string, installationID int64, defaultBranch string) error
	UnlinkRepository(ctx context.Context, tenantID, projectName string) error
	GetByProject(ctx context.Context, tenantID, projectName string) (*ProjectRepository, error)
	GetByRepo(ctx context.Context, repoFullName string) (*ProjectRepository, error)
	SaveInstallation(ctx context.Context, id int64, accountLogin, accountType string, tenantID *string) error
}

// RepositoryHandler handles project↔GitHub repository linking.
type RepositoryHandler struct {
	store RepositoryStore
}

// NewRepositoryHandler creates a new RepositoryHandler.
func NewRepositoryHandler(store RepositoryStore) *RepositoryHandler {
	return &RepositoryHandler{store: store}
}

type connectRequest struct {
	Repo           string `json:"repo"`            // "owner/repo"
	InstallationID int64  `json:"installation_id"`
	Branch         string `json:"branch,omitempty"` // default: "main"
}

type connectResponse struct {
	OK         bool   `json:"ok"`
	Project    string `json:"project"`
	Repo       string `json:"repo"`
	Branch     string `json:"branch"`
	AutoDeploy bool   `json:"auto_deploy"`
}

// Connect links a GitHub repository to a LoopZero project (POST /api/v1/projects/{name}/connect).
func (h *RepositoryHandler) Connect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	if uc.Role != "owner" && uc.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"code":    "FORBIDDEN",
			"message": "Only team owners and admins can connect repositories.",
		})
		return
	}

	projectName := chi.URLParam(r, "name")

	var req connectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INTERNAL_ERROR", "message": "invalid request body"})
		return
	}

	if req.Repo == "" || req.InstallationID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "repo and installation_id are required",
		})
		return
	}

	branch := req.Branch
	if branch == "" {
		branch = "main"
	}

	// Check if repo is already linked to another project
	existing, err := h.store.GetByRepo(ctx, req.Repo)
	if err != nil {
		slog.ErrorContext(ctx, "repository lookup failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": "internal error"})
		return
	}
	if existing != nil && (existing.TenantID != uc.TenantID || existing.ProjectName != projectName) {
		writeJSON(w, http.StatusConflict, map[string]string{
			"code":    "REPO_ALREADY_LINKED",
			"message": "Repository is already connected to another project.",
		})
		return
	}

	// Ensure installation is saved
	tenantID := uc.TenantID
	if err := h.store.SaveInstallation(ctx, req.InstallationID, "", "User", &tenantID); err != nil {
		slog.ErrorContext(ctx, "save installation failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": "internal error"})
		return
	}

	// Link (idempotent — if already linked to same project, no-op via unique constraint)
	if err := h.store.LinkRepository(ctx, uc.TenantID, projectName, req.Repo, req.InstallationID, branch); err != nil {
		slog.ErrorContext(ctx, "link repository failed", "error", err, "repo", req.Repo)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": "failed to connect repository"})
		return
	}

	slog.InfoContext(ctx, "repository connected", "project", projectName, "repo", req.Repo, "branch", branch)

	writeJSON(w, http.StatusOK, connectResponse{
		OK:         true,
		Project:    projectName,
		Repo:       req.Repo,
		Branch:     branch,
		AutoDeploy: true,
	})
}

// Disconnect unlinks a GitHub repository from a LoopZero project (DELETE /api/v1/projects/{name}/connect).
func (h *RepositoryHandler) Disconnect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	if uc.Role != "owner" && uc.Role != "admin" {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"code":    "FORBIDDEN",
			"message": "Only team owners and admins can disconnect repositories.",
		})
		return
	}

	projectName := chi.URLParam(r, "name")

	if err := h.store.UnlinkRepository(ctx, uc.TenantID, projectName); err != nil {
		// AUDIT-21: map "not linked" to 404 instead of silent success.
		// RepositoryStore returns a sentinel we match by message suffix
		// to keep routes free of a direct db package dependency (same
		// pattern used in AUDIT-12 for environments).
		if strings.Contains(err.Error(), "no repository linked to project") {
			writeLoopZeroError(w, loopzeroerrors.NewRepositoryNotLinked(projectName))
			return
		}
		slog.ErrorContext(ctx, "unlink repository failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": "failed to disconnect"})
		return
	}

	slog.InfoContext(ctx, "repository disconnected", "project", projectName)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"project": projectName,
	})
}

// GetConnection returns the linked repository for a project (GET /api/v1/projects/{name}/connect).
func (h *RepositoryHandler) GetConnection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	projectName := chi.URLParam(r, "name")

	repo, err := h.store.GetByProject(ctx, uc.TenantID, projectName)
	if err != nil {
		slog.ErrorContext(ctx, "get connection failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": "internal error"})
		return
	}
	if repo == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"code":    "NOT_CONNECTED",
			"message": "No repository connected to this project.",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"project":    repo.ProjectName,
		"repo":       repo.RepoFullName,
		"provider":   repo.Provider,
		"branch":     repo.DefaultBranch,
		"auto_deploy": repo.AutoDeploy,
		"connected_at": repo.CreatedAt,
	})
}
