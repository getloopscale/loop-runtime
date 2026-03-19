package routes

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// environmentRegex validates environment names (DA-10).
// Lowercase, starts with letter, alphanumeric + hyphens, max 63 chars.
var environmentRegex = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// AppInput describes a single app from the CLI request.
type AppInput struct {
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"`      // "server", "worker", "frontend"
	Port      int    `json:"port,omitempty"`
	Path      string `json:"path,omitempty"`
	Framework string `json:"framework,omitempty"` // "express", "go", "fastapi", etc.
}

// SubmitBuildInput contains the inputs for submitting a new build via the route handler.
type SubmitBuildInput struct {
	TenantID      string
	ProjectID     string
	Environment   string
	CommitSHA     string
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

// BuildStatusResponse is the JSON response for GET /builds/:id.
type BuildStatusResponse struct {
	BuildID     string `json:"build_id"`
	ProjectID   string `json:"project_id"`
	Environment string `json:"environment"`
	Status      string `json:"status"`
	CommitSHA   string `json:"commit_sha"`
	CreatedBy   string `json:"created_by"`
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

	resp, err := h.orch.SubmitBuild(ctx, SubmitBuildInput{
		TenantID:      uc.TenantID,
		ProjectID:     req.ProjectID,
		Environment:   req.Environment,
		CommitSHA:     req.CommitSHA,
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

	// Fallback for unexpected errors
	writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
}
