// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"net/http"
	"time"

	"github.com/getloopscale/loopzero/api/internal/httpio"
	"github.com/getloopscale/loopzero/api/internal/pagination"
	"github.com/getloopscale/loopzero/api/internal/ports"

	"github.com/go-chi/chi/v5"

	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
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
	// T5.3 LIVE-bug iter 20 fix (2026-06-08): per-build apps snapshot was
	// missing from builds list response, so the dashboard /projects/:name
	// Apps card list rendered empty even when apps_spec was populated.
	// Apps mirror the apps_spec JSONB column (minus internal fields).
	// Reuses BuildAppInfo (declared in builds.go) — same shape used by
	// the BuildStatusResponse for /builds/{id} singular endpoint.
	Apps []BuildAppInfo
}

// BuildListStore abstracts build list retrieval (DIP).
type BuildListStore interface {
	ListByProjectAndEnv(ctx context.Context, tenantID, projectID, env string, limit, offset int) ([]BuildListItem, int, error)
	// ListEnvsForProject returns the distinct set of environments the
	// project has built into, ordered by most-recently-used. Empty
	// slice (not nil) for cold projects. Post plan
	// 2026-06-08-dashboard-screens-review-fixes T2.2 + ADR D3.
	ListEnvsForProject(ctx context.Context, tenantID, projectName string) ([]string, error)
}

// BuildListHandler provides HTTP handlers for listing builds.
type BuildListHandler struct {
	builds BuildListStore
	// projects is the EC-2 SEPA-CRITICAL cross-tenant gate. Nil
	// disables the gate (legacy List path stays unchanged for callers
	// constructed via NewBuildListHandler — the gate is a T2.2 addition
	// only relevant for the new /build-envs endpoint).
	projects ports.ProjectStore
}

// NewBuildListHandler creates a new BuildListHandler (legacy constructor —
// /build-envs not exposed because the cross-tenant gate is not wired).
func NewBuildListHandler(builds BuildListStore) *BuildListHandler {
	return &BuildListHandler{builds: builds}
}

// NewBuildListHandlerWithGate creates a BuildListHandler with the EC-2
// SEPA-CRITICAL cross-tenant gate wired. Plan
// 2026-06-08-dashboard-screens-review-fixes T2.2 DoD requires this
// constructor to be used in production setup — `setup_server.go` MUST
// pass the ProjectStore so /build-envs lookups go through GetByName
// BEFORE the query is even attempted. Without the gate, a tenant could
// probe `/api/v1/projects/<other-tenant-project>/build-envs` and
// observe metadata (env names) that doesn't belong to them.
func NewBuildListHandlerWithGate(builds BuildListStore, projects ports.ProjectStore) *BuildListHandler {
	return &BuildListHandler{builds: builds, projects: projects}
}

// buildListResponse is the JSON response for GET /api/v1/projects/{name}/builds.
type buildListResponse struct {
	Builds []BuildStatusResponse `json:"builds"`
	Total  int                   `json:"total"`
}

// List handles GET /api/v1/projects/{name}/builds.
// Query params: env (optional), limit (default 20, max 50), offset (default 0).
func (h *BuildListHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("project name is required"))
		return
	}

	// Red Team loopzero-cloud#92 absorption (2026-06-27): missing-parent-guard.
	// Without this check, GET /projects/<nonexistent>/builds returned 200
	// with an empty list, so the dashboard's sub-views (Deployments page)
	// rendered functional UI for a deleted/non-existent project. Same EC-2
	// SEPA-CRITICAL pattern as BuildEnvs below: a tenant probing
	// /projects/<foo-of-other-tenant>/builds gets the canonical
	// PROJECT_NOT_FOUND (no existence leak) — `projects.GetByName` already
	// scopes by uc.TenantID.
	if h.projects != nil {
		project, err := h.projects.GetByName(ctx, uc.TenantID, name)
		if err != nil {
			httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
			return
		}
		if project == nil {
			httpio.WriteLoopZeroError(w, loopzeroerrors.NewProjectNotFound(name))
			return
		}
	}

	env := r.URL.Query().Get("env")
	limit := pagination.ParseIntParam(r.URL.Query().Get("limit"), 20)
	offset := pagination.ParseIntParam(r.URL.Query().Get("offset"), 0)

	builds, total, err := h.builds.ListByProjectAndEnv(ctx, uc.TenantID, name, env, limit, offset)
	if err != nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
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
			// T5.3 LIVE-bug iter 20 (2026-06-08): copy the per-build apps
			// snapshot. Adapter parses apps_spec JSONB into b.Apps; here we
			// just forward the slice through. omitempty drops the key when
			// the build's apps_spec was empty/missing.
			Apps: b.Apps,
		}
		if b.StartedAt != nil {
			item.StartedAt = b.StartedAt.UTC().Format(time.RFC3339)
		}
		if b.FinishedAt != nil {
			item.FinishedAt = b.FinishedAt.UTC().Format(time.RFC3339)
		}
		resp.Builds = append(resp.Builds, item)
	}

	httpio.WriteJSON(w, http.StatusOK, resp)
}

// buildEnvsResponse is the JSON shape for GET /api/v1/projects/{name}/build-envs.
type buildEnvsResponse struct {
	Envs  []string `json:"envs"`
	Total int      `json:"total"`
}

// BuildEnvs handles GET /api/v1/projects/{name}/build-envs.
// Returns the distinct envs the project has built into, ordered by
// most-recently-used. Feeds the dashboard /deployments env-filter chips
// (replaces the hardcoded production/staging/preview/all literals per
// ADR D3).
//
// EC-2 SEPA-CRITICAL: cross-tenant probes return 404 PROJECT_NOT_FOUND
// (same shape as "project does not exist anywhere") to prevent
// existence leak. The gate is applied BEFORE the build-envs query
// runs — a tenant cannot even reach the underlying query if the project
// does not belong to them.
func (h *BuildListHandler) BuildEnvs(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("project name is required"))
		return
	}

	// EC-2 gate: project lookup. Nil = project does not belong to this
	// tenant (or doesn't exist anywhere — same response shape).
	if h.projects != nil {
		project, err := h.projects.GetByName(ctx, uc.TenantID, name)
		if err != nil {
			httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
			return
		}
		if project == nil {
			httpio.WriteLoopZeroError(w, loopzeroerrors.NewProjectNotFound(name))
			return
		}
	}

	envs, err := h.builds.ListEnvsForProject(ctx, uc.TenantID, name)
	if err != nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if envs == nil {
		envs = []string{}
	}

	httpio.WriteJSON(w, http.StatusOK, buildEnvsResponse{
		Envs:  envs,
		Total: len(envs),
	})
}
