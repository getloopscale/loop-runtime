// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
	"github.com/loopzero/loopzero/pkg/naming"
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
	Scaling         *StatusBuildScaling // Autoscaling config from the build's app spec (nil = no autoscaling)
}

// StatusBuildScaling holds the autoscaling configuration extracted from the build's
// apps_spec. Used to populate the scaling field in the status response.
// Replicas = MinReplicas (configured baseline); actual count requires K8s API (future).
type StatusBuildScaling struct {
	MinReplicas int
	MaxReplicas int
}

// StatusReleaseStore abstracts active release retrieval for the status handler (DIP).
type StatusReleaseStore interface {
	GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*StatusRelease, error)
}

// StatusRelease is the release representation used by the status handler.
type StatusRelease struct {
	ID        string
	BuildID   string
	Status    string
	GitCommit string
	CreatedAt time.Time
	// EntryApp is the name of the apps_spec's first server/frontend app, used
	// to emit the canonical public URL (`<tenant>-<project>-<entryApp>-<env>.<publicDomain>`).
	// Empty when the release has no rendered apps (rare). Populated by the
	// StatusReleaseStore adapter (parses release.Apps json.RawMessage as
	// []db.BuildAppSpec). FAANG canonical (J-25 sponsor walkthrough catch
	// 2026-06-12): status.url was previously emitting a simplified formula
	// `<project>-<env>.<baseDomain>` that did NOT match the real
	// IngressRoute Host emitted by renderer_helpers.appHostname() — so the
	// "Visit" link in the dashboard hero card never resolved to anything.
	EntryApp string
}

// StatusProjectStore abstracts project lookup for the status handler (DIP).
type StatusProjectStore interface {
	GetByName(ctx context.Context, tenantID, name string) (*ProjectResult, error)
}

// StatusHandler provides HTTP handlers for project status operations.
type StatusHandler struct {
	projects     StatusProjectStore
	builds       StatusBuildStore
	releases     StatusReleaseStore
	baseDomain   string
	publicDomain string // operator-configured public wildcard for status.url (FAANG canonical J-25; mirrors PreviewPublicDomain)
}

// NewStatusHandler creates a new StatusHandler.
//
// publicDomain is the operator-configured wildcard root for the public URL
// (e.g. `prod-dev.loopzero.example` for dev-public). When empty, the handler
// falls back to baseDomain (internal hostname). Parallels PreviewPublicDomain
// + BaseDomain split at PreviewService — closes catches #21+#22 from T6.2
// session addendum 2026-06-12 (J-25 sponsor walkthrough LIVE catch).
func NewStatusHandler(projects StatusProjectStore, builds StatusBuildStore, releases StatusReleaseStore, baseDomain, publicDomain string) *StatusHandler {
	return &StatusHandler{
		projects:     projects,
		builds:       builds,
		releases:     releases,
		baseDomain:   baseDomain,
		publicDomain: publicDomain,
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
	BuildID   string `json:"build_id"`
	Status    string `json:"status"`
	GitCommit string `json:"git_commit"`
	CreatedAt string `json:"created_at"`
}

// scalingStatusResponse is the scaling info in the status response.
type scalingStatusResponse struct {
	Replicas     int  `json:"replicas"`
	Min          int  `json:"min"`
	Max          int  `json:"max"`
	ScaledToZero bool `json:"scaled_to_zero"`
}

// rolloutStatusResponse describes the canary rollout state for the active
// release (argo-rollouts-progressive-delivery-plan T4.3). Present only when
// progressive delivery is enabled and the release has rollout data.
//
// Phase values: "Healthy" | "Progressing" | "Paused" | "Degraded" | "Aborted".
// AnalysisStatus values: "running" | "successful" | "failed" | "inconclusive".
type rolloutStatusResponse struct {
	Phase          string `json:"phase"`
	CurrentStep    int    `json:"current_step,omitempty"`
	TotalSteps     int    `json:"total_steps,omitempty"`
	CanaryWeight   int    `json:"canary_weight,omitempty"`
	AnalysisStatus string `json:"analysis_status,omitempty"`
}

// statusResponse is the JSON response for GET /api/v1/projects/{name}/status.
type statusResponse struct {
	Project       string                 `json:"project"`
	Environment   string                 `json:"environment"`
	LastBuild     *lastBuildResponse     `json:"last_build"`
	ActiveRelease *activeReleaseResponse `json:"active_release"`
	SyncStatus    string                 `json:"sync_status"`
	URL           string                 `json:"url"`
	Scaling       *scalingStatusResponse `json:"scaling,omitempty"`
	// Rollout is the progressive-delivery state for the active release.
	// Optional/null when:
	//   - release used legacy Deployment manifests (D8 fallback path), OR
	//   - rollout state couldn't be queried (degraded mode), OR
	//   - the release predates progressive delivery rollout (legacy data).
	// argo-rollouts-progressive-delivery-plan T4.3.
	Rollout *rolloutStatusResponse `json:"rollout,omitempty"`
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

	// Backend defaults env to "production" so dashboard / CLI consumers
	// can call /projects/{name}/status without remembering the canonical
	// environment name. Callers querying preview / staging MUST pass
	// ?env=<name> explicitly. The URL still answers the question
	// "what's the live status of this project?" -- which in almost every
	// deploy in the platform means production.
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

	// Get last build for this project+env.
	// Builds use the project NAME (from CLI) as project_id, not the internal UUID.
	// This is a known inconsistency — builds are keyed by name, projects table by UUID.
	builds, err := h.builds.ListByProject(r.Context(), uc.TenantID, name, 50, 0)
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

	// Get active release — uses project name (consistent with build records)
	release, err := h.releases.GetActiveByEnv(r.Context(), uc.TenantID, name, env)
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if release != nil {
		resp.ActiveRelease = &activeReleaseResponse{
			ID:        release.ID,
			BuildID:   release.BuildID,
			Status:    release.Status,
			GitCommit: release.GitCommit,
			CreatedAt: release.CreatedAt.UTC().Format(time.RFC3339),
		}
		resp.SyncStatus = "synced"
	}

	// Scaling info — extracted from the last build's app spec.
	// Uses configured min/max replicas. Actual replica count requires K8s API (future).
	if resp.LastBuild != nil {
		for i := range builds {
			if builds[i].Environment == env && builds[i].Scaling != nil {
				s := builds[i].Scaling
				replicas := s.MinReplicas
				resp.Scaling = &scalingStatusResponse{
					Replicas:     replicas,
					Min:          s.MinReplicas,
					Max:          s.MaxReplicas,
					ScaledToZero: s.MinReplicas == 0 && s.MaxReplicas > 0,
				}
				break
			}
		}
	}

	// Build URL — FAANG canonical (J-25 sponsor walkthrough catch 2026-06-12).
	//
	// Per ADR D1 of plan 2026-06-12-dashboard-journey-j25-live-url-discoverability,
	// status.url MUST emit the SAME hostname formula that the IngressRoute
	// renderer (`renderer_helpers.appHostname` → `pkg/naming.AppHostname`) uses
	// for Traefik routing: `<tenant>-<project>-<entryApp>-<env>.<domain>`.
	//
	// The previous simplified formula `<project>-<env>.<baseDomain>` was missing
	// the tenant prefix and entryApp segment — Traefik returned 404 for every
	// "Visit" click in the dashboard hero card because no IngressRoute matched.
	//
	// Resolution order for the public-facing wildcard domain:
	//   1. h.publicDomain (operator-configured LOOPZERO_PUBLIC_DOMAIN, e.g.
	//      `prod-dev.loopzero.example` for dev-public). When set + release exists +
	//      entryApp resolved, emit the canonical public URL routed by Caddy
	//      wildcard block to Traefik with Host rewrite.
	//   2. Empty when no active release OR entryApp couldn't be resolved —
	//      dashboard renders "URL pending" badge (per ADR D3).
	if release != nil && release.EntryApp != "" && h.publicDomain != "" {
		resp.URL = naming.AppURL(uc.TenantID, name, release.EntryApp, env, h.publicDomain)
	}

	writeJSON(w, http.StatusOK, resp)
}
