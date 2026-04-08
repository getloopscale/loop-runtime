// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/api/internal/validation"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// environmentRegex validates environment names (DA-10).
// Lowercase, starts with letter, alphanumeric + hyphens, max 63 chars.
var environmentRegex = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// AppInput describes a single app from the CLI request.
type AppInput struct {
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"`       // "server", "worker", "frontend"
	Port      int    `json:"port,omitempty"`
	Path      string `json:"path,omitempty"`
	Framework string `json:"framework,omitempty"`  // "express", "go", "fastapi", etc.
	Build     string `json:"build,omitempty"`       // "dockerfile", "buildpack", "custom"
	Rendering string `json:"rendering,omitempty"`   // "static" or "server" (Sprint 10)
	Secrets   []string `json:"secrets,omitempty"`
}

// SubmitBuildInput contains the inputs for submitting a new build via the route handler.
type SubmitBuildInput struct {
	TenantID      string
	ProjectID     string
	Environment   string
	CommitSHA     string
	NoCache       bool // Skip BuildKit layer cache (full rebuild)
	Apps          []AppInput
	CreatedBy     string
	CorrelationID string
}

// SubmitBuildResult is returned after a build is successfully submitted.
type SubmitBuildResult struct {
	BuildID   string `json:"build_id"`
	StreamURL string `json:"stream_url"`
}

// BuildCallbackApp describes a built app in the callback payload.
type BuildCallbackApp struct {
	Name        string `json:"name"`
	ImageDigest string `json:"image_digest"`
	URL         string `json:"url"`
	Type        string `json:"type,omitempty"` // "server", "worker", "frontend"
	DurationMs  int    `json:"duration_ms"`
}

// BuildCallbackInput is the callback payload from the build system.
type BuildCallbackInput struct {
	Status    string             `json:"status"`
	Error     string             `json:"error,omitempty"`
	Apps      []BuildCallbackApp `json:"apps"`
	Manifests string             `json:"manifests,omitempty"` // raw K8s YAML from build pipeline
}

// BuildOrchestratorService defines the interface the route handler needs
// from the build orchestrator. Follows DIP — handler depends on abstraction,
// using route-local types to avoid import cycles with services/db packages.
type BuildOrchestratorService interface {
	SubmitBuild(ctx context.Context, req SubmitBuildInput) (*SubmitBuildResult, error)
	GetBuildStatus(ctx context.Context, buildID string) (*BuildStatusResponse, error)
	OnBuildCompleted(ctx context.Context, buildID string, result BuildCallbackInput) error
	IsBuildTerminal(ctx context.Context, buildID string) (bool, error)
	CancelBuild(ctx context.Context, buildID string) error
	// GetBuildTenantID returns the tenant that owns a build — used for authorization.
	GetBuildTenantID(ctx context.Context, buildID string) (string, error)
}

// BuildAppInfo describes a built app in a build status response.
type BuildAppInfo struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

// BuildStatusResponse is the JSON response for GET /builds/:id.
type BuildStatusResponse struct {
	BuildID         string         `json:"build_id"`
	ProjectID       string         `json:"project_id"`
	Environment     string         `json:"environment"`
	Status          string         `json:"status"`
	CommitSHA       string         `json:"commit_sha"`
	CreatedBy       string         `json:"created_by"`
	CreatedAt       string         `json:"created_at"`
	StartedAt       string         `json:"started_at,omitempty"`
	FinishedAt      string         `json:"finished_at,omitempty"`
	DurationSeconds *int           `json:"duration_seconds,omitempty"`
	ErrorMessage    string         `json:"error,omitempty"`
	Apps            []BuildAppInfo `json:"apps,omitempty"`
}

// BuildHandler provides HTTP handlers for build operations.
//
// C-01 fix: The callback handler has been removed. Build completion is now
// detected by the BuildReconcileLoop which polls Argo Workflow CRDs from the
// control plane. The build cluster no longer calls the API.
type BuildHandler struct {
	orch   BuildOrchestratorService
	logger *slog.Logger
}

// NewBuildHandler creates a new BuildHandler.
func NewBuildHandler(orch BuildOrchestratorService, logger *slog.Logger) *BuildHandler {
	return &BuildHandler{
		orch:   orch,
		logger: logger,
	}
}

// createBuildRequest is the JSON request body for POST /api/v1/builds.
type createBuildRequest struct {
	ProjectID   string     `json:"project_id"`
	Environment string     `json:"environment"`
	CommitSHA   string     `json:"commit_sha,omitempty"`
	NoCache     bool       `json:"no_cache,omitempty"` // Skip BuildKit layer cache (full rebuild)
	Apps        []AppInput `json:"apps,omitempty"`
}

// Create handles POST /api/v1/builds.
// Auth required. Idempotency-Key middleware applied externally.
func (h *BuildHandler) Create(w http.ResponseWriter, r *http.Request) {
	// C-02: Enforce request deadline so handlers don't hang indefinitely.
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	var req createBuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInvalidRequest)
		return
	}

	// Validate required fields
	if req.ProjectID == "" {
		writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"MISSING_FIELD",
			"Project name is required. Check your loopzero.yaml.",
			400,
		))
		return
	}
	if req.Environment == "" {
		writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"MISSING_FIELD",
			"Environment is required",
			400,
		))
		return
	}

	// Environment validation (DA-10): lowercase only
	if !environmentRegex.MatchString(req.Environment) {
		writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"INVALID_ENVIRONMENT",
			"Environment names must be lowercase.",
			400,
		))
		return
	}

	// SEC-3 fix: Validate app paths before they reach shell scripts in the build pipeline.
	// Prevents path traversal and command injection via crafted app.path values.
	// CODE-001 fix: Also validate app names — they flow into shell scripts in
	// loopzeroPacksDockerfileStep() and buildctl arguments.
	for _, app := range req.Apps {
		if err := validation.ValidateAppName(app.Name); err != nil {
			writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
				"INVALID_APP_NAME",
				fmt.Sprintf("Invalid name for app: %s", err.Error()),
				400,
			))
			return
		}
		if err := validation.ValidateAppPath(app.Path); err != nil {
			writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
				"INVALID_APP_PATH",
				fmt.Sprintf("Invalid path for app %q: %s", app.Name, err.Error()),
				400,
			))
			return
		}
	}

	// V5: Server-side framework validation — reject unsupported frameworks before pipeline.
	// Defense-in-depth: CLI validates first, but API is the final gate.
	for _, app := range req.Apps {
		if isUnknownFramework(app.Framework) && app.Build != "dockerfile" {
			writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
				"UNSUPPORTED_FRAMEWORK",
				fmt.Sprintf("App '%s': framework '%s' requires 'build: dockerfile'. Set build to 'dockerfile' and provide a Dockerfile, or use a supported framework.", app.Name, app.Framework),
				422,
			))
			return
		}
	}

	resp, err := h.orch.SubmitBuild(ctx, SubmitBuildInput{
		TenantID:      uc.TenantID,
		ProjectID:     req.ProjectID,
		Environment:   req.Environment,
		CommitSHA:     req.CommitSHA,
		NoCache:       req.NoCache,
		Apps:          req.Apps,
		CreatedBy:     uc.UserID,
		CorrelationID: r.Header.Get("Correlation-ID"), //nolint:contextcheck // header from original request
	})
	if err != nil {
		mapBuildError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, resp)
}

// Get handles GET /api/v1/builds/:id.
// SEC-4: Validates that the authenticated user's tenant owns the build.
func (h *BuildHandler) Get(w http.ResponseWriter, r *http.Request) {
	// MOD-2: Enforce timeout on read-only handlers to prevent slowloris.
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	buildID := chi.URLParam(r, "id")

	// SEC-4: Verify tenant ownership before returning build data.
	// Returns 404 (not 403) to avoid leaking build existence across tenants.
	buildTenantID, err := h.orch.GetBuildTenantID(ctx, buildID)
	if err != nil {
		mapBuildError(w, err)
		return
	}
	if buildTenantID != uc.TenantID {
		writeLoopZeroError(w, loopzeroerrors.ErrBuildNotFound)
		return
	}

	resp, err := h.orch.GetBuildStatus(ctx, buildID)
	if err != nil {
		mapBuildError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// Cancel handles POST /api/v1/builds/:id/cancel.
// Validates auth and tenant ownership, delegates to orchestrator which checks
// terminal state, cancels the workflow, and transitions the build to CANCELLED.
func (h *BuildHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	// C-02: Enforce request deadline so handlers don't hang indefinitely.
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	buildID := chi.URLParam(r, "id")

	// SEC-4: Verify tenant ownership before allowing cancel.
	// Returns 404 (not 403) to avoid leaking build existence across tenants.
	buildTenantID, err := h.orch.GetBuildTenantID(ctx, buildID)
	if err != nil {
		mapBuildError(w, err)
		return
	}
	if buildTenantID != uc.TenantID {
		writeLoopZeroError(w, loopzeroerrors.ErrBuildNotFound)
		return
	}

	if err := h.orch.CancelBuild(ctx, buildID); err != nil {
		mapBuildError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status": "cancelled",
	})
}

// mapBuildError maps orchestrator errors to HTTP responses.
func mapBuildError(w http.ResponseWriter, err error) {
	var loopzeroErr *loopzeroerrors.LoopZeroError
	if errors.As(err, &loopzeroErr) {
		// DA-18: Override message for DEPLOY_IN_PROGRESS
		if errors.Is(err, loopzeroerrors.ErrDeployInProgress) {
			writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
				"DEPLOY_IN_PROGRESS",
				"A deploy is already in progress for this environment.",
				409,
			))
			return
		}
		writeLoopZeroError(w, loopzeroErr)
		return
	}

	// F-03: Log unexpected errors for debugging — the audit middleware only logs
	// status=500 at INFO level, which is useless for diagnosing root causes.
	slog.Error("unexpected error in build handler",
		"error", err.Error(),
		"type", fmt.Sprintf("%T", err),
	)

	// Fallback for unexpected errors
	writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
}

// isUnknownFramework returns true if the framework is not in the known list.
// Mirrors resolveLanguageStrategy() in services/build_plan.go — kept in sync.
// V5: Server-side validation to reject unknown frameworks before pipeline.
func isUnknownFramework(framework string) bool {
	switch framework {
	case "nextjs", "next", "vite", "remix", "nestjs", "express", "node",
		"react", "vue", "svelte", "astro", "angular", "fastify",
		"gin", "go",
		"fastapi", "django", "python",
		"sinatra", "rails", "ruby",
		"spring", "spring-boot", "java",
		"laravel", "slim", "php",
		"axum", "actix", "rocket", "rust",
		"custom", "unknown", "":
		return false
	default:
		return true
	}
}
