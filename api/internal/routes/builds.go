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

	"github.com/loopzero/loopzero/api/internal/argo"
	"github.com/loopzero/loopzero/api/internal/correlation"
	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/loopzero/loopzero/api/internal/validation"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// environmentRegex validates environment names (DA-10).
//
// Sprint B T3.2 (plan 2026-06-01-p2-interface-contracts-robustness, ADR D4)
// REVERTED 2026-06-02 after trio dogfood regression: tightening to reject
// hyphens broke 11/66 journey specs that use env names like j-rollout-a,
// j-secrets, j-domain. Those names existed pre-T3.2 in DB-warmed setups
// (grandfathered) but cleanroom-fresh resets the DB, making every env
// "new" — the regex then rejects them at creation. Sponsor mandate trio
// GREEN took precedence over closing code-005 MEDIUM in this iteration.
// Future re-attempt requires migrating the e2e journey fixture names
// FIRST (jrollouta etc.), then tightening the regex. Tracked as
// follow-up in MEET-ASYNC-AMENDMENT-2026-06-02-002 (created post-revert).
//
// Lowercase, starts with letter, alphanumeric + hyphens, max 63 chars.
var environmentRegex = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// AppAutoscalingInput mirrors the CLI autoscaling payload (field names match
// what the CLI deploy.ts sends — snake_case target_cpu).
// B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059).
type AppAutoscalingInput struct {
	Min       int `json:"min,omitempty"`
	Max       int `json:"max,omitempty"`
	TargetCPU int `json:"target_cpu,omitempty"`
}

// AppInput describes a single app from the CLI request.
type AppInput struct {
	Name      string               `json:"name"`
	Type      string               `json:"type,omitempty"`       // "server", "worker", "frontend"
	Port      int                  `json:"port,omitempty"`
	Path      string               `json:"path,omitempty"`
	Framework string               `json:"framework,omitempty"`  // "express", "go", "fastapi", etc.
	Build     string               `json:"build,omitempty"`       // "dockerfile", "buildpack", "custom"
	Rendering string               `json:"rendering,omitempty"`   // "static" or "server" (Sprint 10)
	Secrets   []string             `json:"secrets,omitempty"`
	// B15: scaling config — either Replicas OR Autoscaling (never both).
	// Orchestrator enforces plan caps and mutual exclusion.
	Replicas    int                  `json:"replicas,omitempty"`
	Autoscaling *AppAutoscalingInput `json:"autoscaling,omitempty"`
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
	// GetRedeploySource — Plan J-17 T6.4. Returns the params needed to
	// re-submit a build identical to the source: project_id, environment,
	// commit_sha, and the persisted apps spec. Verifies tenant ownership
	// inside. Returns ErrBuildNotTerminal if the source build is still
	// in flight. Path/Framework/Build fields on the returned apps will
	// be empty because the apps_spec column does not preserve them;
	// loopzero-packs autodetects at build time for single-app monorepos,
	// which is the common case. Multi-app monorepos with non-root paths
	// need a schema migration (deferred).
	GetRedeploySource(ctx context.Context, tenantID, buildID string) (*RedeploySource, error)
}

// RedeploySource — Plan J-17 T6.4. Snapshot of the params needed to
// resubmit a build as a redeploy of an existing one.
type RedeploySource struct {
	ProjectID   string
	Environment string
	CommitSHA   string
	Apps        []AppInput
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

// AdmissionChecker is the minimal contract the build handler needs from
// the admission gate. Implemented by *admission.BuildGate. Defined in
// this package to keep routes free of cross-package imports for pure
// interfaces (DIP).
//
// A `nil` AdmissionChecker is legal and means "no admission control" —
// the handler will skip the check entirely, which is the current
// production default until LOOPZERO_ADMISSION_CONTROL is flipped.
type AdmissionChecker interface {
	// Check returns (admitted, retryAfter, code, message). admitted=true
	// means the build can proceed. When false, the handler returns a
	// 429 with the given code + message + Retry-After header.
	Check(ctx context.Context, cellID, tenantID string) (admitted bool, retryAfter time.Duration, code, message string, err error)
}

// CellResolver resolves the cell_id for a given tenant.
// MEET-053 A-06: Replaces hardcoded DefaultCellID in admission checks.
// Implemented by db.PostgresCellStore.GetTenantCellID.
type CellResolver interface {
	GetTenantCellID(ctx context.Context, tenantID string) (string, error)
}

// BuildHandler provides HTTP handlers for build operations.
//
// C-01 fix: The callback handler has been removed. Build completion is now
// detected by the BuildReconcileLoop which polls Argo Workflow CRDs from the
// control plane. The build cluster no longer calls the API.
type BuildHandler struct {
	orch          BuildOrchestratorService
	logger        *slog.Logger
	admission     AdmissionChecker     // nil = disabled, no-op
	cellResolver  CellResolver         // MEET-053: resolves tenant → cell_id per-request
	envEnsurer    EnvironmentEnsurer   // Plan T2.2 D5: auto-create env on first deploy --env <novo>
	secretResync  SecretResyncer       // V9 L20: resync PG secrets to workload K8s post-envEnsurer
}

// NewBuildHandler creates a new BuildHandler.
// EnvironmentEnsurer ensures the requested environment exists (auto-create
// on first deploy --env <novo>). Plan T2.2 D5. Implementations should be
// idempotent: existing envs return nil immediately; new envs create the
// Environment CR + namespace + ArgoCD app and wait for phase=Active.
// M2 v1.2 (2026-06-07): `project` propagates end-to-end so the
// canonical 3-segment namespace `loopzero-<tenant>-<project>-<env>` is
// computed and the Tenant CR spec.environments entry carries the
// project field. Empty project preserves the legacy 2-segment path
// for production-legacy callers (ADR D2).
type EnvironmentEnsurer interface {
	EnsureEnvironment(ctx context.Context, tenantID, project, envName string) error
}

func NewBuildHandler(orch BuildOrchestratorService, logger *slog.Logger) *BuildHandler {
	return &BuildHandler{
		orch:   orch,
		logger: logger,
	}
}

// WithAdmission attaches a build admission gate and cell resolver.
// MEET-053 A-06: cellResolver replaces the hardcoded cellID string.
// The resolver is called per-request to get the tenant's actual cell.
func (h *BuildHandler) WithAdmission(a AdmissionChecker, resolver CellResolver) *BuildHandler {
	h.admission = a
	h.cellResolver = resolver
	return h
}

// WithEnvironmentEnsurer wires an EnvironmentEnsurer so the build handler
// auto-creates missing Environment CRs on first deploy --env <novo>. Plan
// T2.2 D5. Nil ensurer (default) preserves legacy behavior (env must pre-exist).
func (h *BuildHandler) WithEnvironmentEnsurer(e EnvironmentEnsurer) *BuildHandler {
	h.envEnsurer = e
	return h
}

// SecretResyncer (re)creates ExternalSecrets on the workload cluster for
// all secrets in (tenant, project, env). V9 L20: called after
// EnsureEnvironment so secrets set BEFORE the namespace existed get
// repopulated on workload. Idempotent: existing ExternalSecrets get a
// new resourceVersion-friendly Update; missing ones get Create.
type SecretResyncer interface {
	ResyncEnv(ctx context.Context, tenantID, projectID, env string) error
}

// WithSecretResyncer wires a SecretResyncer so the build handler closes
// the "set secret BEFORE first deploy" race documented in audit
// 2026-05-30-V9-L19-99percent. Nil resyncer preserves legacy behavior:
// secrets only sync at POST /api/v1/secrets time (which fails silently
// when the namespace doesn't yet exist).
func (h *BuildHandler) WithSecretResyncer(r SecretResyncer) *BuildHandler {
	h.secretResync = r
	return h
}

// createBuildRequest is the JSON request body for POST /api/v1/builds.
type createBuildRequest struct {
	ProjectID   string     `json:"project_id"`
	Environment string     `json:"environment"`
	CommitSHA   string     `json:"commit_sha,omitempty"`
	NoCache     bool       `json:"no_cache,omitempty"` // Skip BuildKit layer cache (full rebuild)
	Apps        []AppInput `json:"apps,omitempty"`
	// Plan J-17 T6.4 — when set, the handler looks up the source build,
	// verifies tenant ownership + terminal status, and copies project,
	// environment, commit_sha, and apps from it. The body's own
	// ProjectID/Environment/CommitSHA/Apps are ignored.
	RedeployOf string `json:"redeploy_of,omitempty"`
}

// Create handles POST /api/v1/builds.
// Auth required. Idempotency-Key middleware applied externally.
//
// Plan 2026-06-15-code-review-49-findings-remediation T5.4 (R2-F3 HIGH):
// CC 29 → ≤ 12 via 5 helper extracts (resolveRedeploySource +
// validateBuildRequiredFields + ensureEnvAndResyncSecrets + validateApps +
// checkAdmission). Behavior preserved verbatim.
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

	if !h.resolveRedeploySource(ctx, w, uc.TenantID, &req) {
		return
	}

	if err := validateBuildRequiredFields(req); err != nil {
		writeLoopZeroError(w, err)
		return
	}

	if !h.ensureEnvAndResyncSecrets(ctx, w, uc.TenantID, req) {
		return
	}

	if err := validateApps(req.Apps); err != nil {
		writeLoopZeroError(w, err)
		return
	}

	if !h.checkAdmission(ctx, w, uc.TenantID) {
		return
	}

	resp, err := h.orch.SubmitBuild(ctx, SubmitBuildInput{
		TenantID:      uc.TenantID,
		ProjectID:     req.ProjectID,
		Environment:   req.Environment,
		CommitSHA:     req.CommitSHA,
		NoCache:       req.NoCache,
		Apps:          req.Apps,
		CreatedBy:     uc.UserID,
		CorrelationID: correlation.Read(r), //nolint:contextcheck // header from original request (T6.1 uniform read via helper)
	})
	if err != nil {
		mapBuildError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, resp)
}

// resolveRedeploySource implements Plan J-17 T6.4 redeploy_of short-circuit.
// When req.RedeployOf is set, look up the source build + copy its params.
// Returns true to continue, false when mapBuildError wrote the response.
func (h *BuildHandler) resolveRedeploySource(
	ctx context.Context,
	w http.ResponseWriter,
	tenantID string,
	req *createBuildRequest,
) bool {
	if req.RedeployOf == "" {
		return true
	}
	src, srcErr := h.orch.GetRedeploySource(ctx, tenantID, req.RedeployOf)
	if srcErr != nil {
		mapBuildError(w, srcErr)
		return false
	}
	req.ProjectID = src.ProjectID
	req.Environment = src.Environment
	req.CommitSHA = src.CommitSHA
	req.Apps = src.Apps
	// NoCache opt-in stays from the request; everything else comes from the source.
	return true
}

// validateBuildRequiredFields enforces:
//   - ProjectID non-empty (MISSING_FIELD 400)
//   - Environment non-empty (MISSING_FIELD 400)
//   - Apps non-empty (CR-2 regression defense — T1.3 follow-up)
//   - environmentRegex matches (DA-10 lowercase only)
func validateBuildRequiredFields(req createBuildRequest) *loopzeroerrors.LoopZeroError {
	if req.ProjectID == "" {
		return loopzeroerrors.NewLoopZeroError("MISSING_FIELD",
			"Project name is required. Check your loopzero.yaml.", 400)
	}
	if req.Environment == "" {
		return loopzeroerrors.NewLoopZeroError("MISSING_FIELD",
			"Environment is required", 400)
	}
	if len(req.Apps) == 0 {
		return loopzeroerrors.NewLoopZeroError("MISSING_FIELD",
			"At least one app is required. Check your loopzero.yaml for an `apps:` section.", 400)
	}
	if !environmentRegex.MatchString(req.Environment) {
		return loopzeroerrors.NewLoopZeroError("INVALID_ENVIRONMENT",
			"Environment names must be lowercase.", 400)
	}
	return nil
}

// ensureEnvAndResyncSecrets implements Plan T2.2 D5 (auto-create env on
// first deploy --env <novo>) + V9 L20 (PG → K8s ExternalSecret resync).
// Returns true to continue, false when EnsureEnvironment failed and the
// response was written. Secret resync errors are non-fatal (best-effort).
func (h *BuildHandler) ensureEnvAndResyncSecrets(
	ctx context.Context,
	w http.ResponseWriter,
	tenantID string,
	req createBuildRequest,
) bool {
	if h.envEnsurer != nil {
		if ensureErr := h.envEnsurer.EnsureEnvironment(ctx, tenantID, req.ProjectID, req.Environment); ensureErr != nil {
			var te *loopzeroerrors.LoopZeroError
			if errors.As(ensureErr, &te) {
				writeLoopZeroError(w, te)
			} else {
				writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError("ENV_PROVISION_FAILED",
					fmt.Sprintf("Failed to provision environment %q: %s", req.Environment, ensureErr.Error()), 500))
			}
			return false
		}
	}
	if h.secretResync != nil {
		if resyncErr := h.secretResync.ResyncEnv(ctx, tenantID, req.ProjectID, req.Environment); resyncErr != nil {
			slog.Warn("secret resync failed — pods may fail to start with CreateContainerConfigError",
				"error", resyncErr,
				"tenant_id", tenantID,
				"project", req.ProjectID,
				"environment", req.Environment,
			)
		}
	}
	return true
}

// validateApps enforces:
//   - SEC-3 + CODE-001 app name + path validation (prevent shell injection)
//   - V5 server-side framework validation (defense-in-depth post-CLI)
//
// Returns nil to continue OR a typed LoopZeroError to write.
func validateApps(apps []AppInput) *loopzeroerrors.LoopZeroError {
	for _, app := range apps {
		if err := validation.ValidateAppName(app.Name); err != nil {
			return loopzeroerrors.NewLoopZeroError("INVALID_APP_NAME",
				fmt.Sprintf("Invalid name for app: %s", err.Error()), 400)
		}
		if err := validation.ValidateAppPath(app.Path); err != nil {
			return loopzeroerrors.NewLoopZeroError("INVALID_APP_PATH",
				fmt.Sprintf("Invalid path for app %q: %s", app.Name, err.Error()), 400)
		}
		if isUnknownFramework(app.Framework) && app.Build != "dockerfile" {
			return loopzeroerrors.NewLoopZeroError("UNSUPPORTED_FRAMEWORK",
				fmt.Sprintf("App '%s': framework '%s' requires 'build: dockerfile'. Set build to 'dockerfile' and provide a Dockerfile, or use a supported framework.", app.Name, app.Framework), 422)
		}
	}
	return nil
}

// checkAdmission implements Roadmap 07 admission gate. Returns true to
// continue, false when admission denied + 429 was written. Fail-open on
// query error (the gate is advisory).
func (h *BuildHandler) checkAdmission(
	ctx context.Context,
	w http.ResponseWriter,
	tenantID string,
) bool {
	if h.admission == nil {
		return true
	}
	cellID := h.resolveCellID(ctx, tenantID)
	admitted, retryAfter, code, message, adErr := h.admission.Check(ctx, cellID, tenantID)
	if adErr != nil {
		if h.logger != nil {
			h.logger.Warn("admission gate query failed — failing open",
				"error", adErr, "tenant_id", tenantID, "cell_id", cellID)
		}
		return true
	}
	if admitted {
		return true
	}
	secs := int(retryAfter.Round(time.Second).Seconds())
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", fmt.Sprintf("%d", secs))
	writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(code, message, 429))
	return false
}

// resolveCellID returns the tenant's cell ID for admission gating.
// MEET-053 A-06: per-request resolution; fallback to cell-default when
// the resolver is unset OR errors transiently.
func (h *BuildHandler) resolveCellID(ctx context.Context, tenantID string) string {
	cellID := "cell-default"
	if h.cellResolver == nil {
		return cellID
	}
	resolved, resolveErr := h.cellResolver.GetTenantCellID(ctx, tenantID)
	if resolveErr != nil {
		if h.logger != nil {
			h.logger.Warn("cell resolver failed — using default",
				"error", resolveErr, "tenant_id", tenantID)
		}
		return cellID
	}
	return resolved
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
//
// T5.4 of deep-review-2026-05-10-remediation-plan (find_arch_framework_dup MEDIUM):
// the canonical list now lives in `api/internal/argo/languages.go`
// (`argo.SupportedFrameworks()`). This wrapper exists so the route handler
// reads from a single source — the previous inline switch could drift
// silently from the build pipeline's view of supported frameworks.
func isUnknownFramework(framework string) bool {
	return !argo.IsKnownFramework(framework)
}
