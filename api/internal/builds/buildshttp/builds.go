// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package buildshttp serves the build endpoints under /api/v1/builds: submitting a
// build, listing and reading them, and the attestation, SBOM and statistics
// read sides.
//
// EIGHTEENTH and last family to leave the flat `routes` package (B-080,
// slice-37), and the largest — five production files for one route prefix,
// because a build has more read surfaces than any other resource here.
//
// One consumer had to alias the import: `internal/adapters/build_store_adapter.go`
// holds a local variable named `builds`, which shadows the package inside the
// function that uses it. The repository's convention suffixes the OTHER party
// (`domainsvc`, `placementsvc`, `previewsvc`), so the routes half became
// `buildroutes` in that file alone.
package buildshttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/httpio"

	"github.com/getloopscale/loop-runtime/api/internal/builds"

	"github.com/go-chi/chi/v5"

	"github.com/getloopscale/loop-runtime/api/internal/correlation"
	auditctx "github.com/getloopscale/loop-runtime/api/internal/middleware/audit"
	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	"github.com/getloopscale/loop-runtime/api/internal/validation"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

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
	Name      string   `json:"name"`
	Type      string   `json:"type,omitempty"` // "server", "worker", "frontend"
	Port      int      `json:"port,omitempty"`
	Path      string   `json:"path,omitempty"`
	Runtime   string   `json:"runtime,omitempty"`   // services.json runtime: "python" | "node" — the build-strategy key (T1, F-dom-2)
	Build     string   `json:"build,omitempty"`     // "dockerfile", "buildpack", "custom"
	Start     string   `json:"start,omitempty"`     // T0.2: declared services.json start command (canonical Dockerfile CMD)
	Rendering string   `json:"rendering,omitempty"` // "server" or empty; NormalizeApp refuses anything else
	Secrets   []string `json:"secrets,omitempty"`
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

// BuildOrchestratorService is what the build ROUTES need from the build
// orchestrator — and only that (ISP). Build completion is not an HTTP
// concern: the reconcile loop drives it through the services/build contract
// (deploycontracts.BuildCallbackRequest), so this port carries no completion
// or terminal-state methods. Route-local types avoid import cycles with the
// services/db packages.
type BuildOrchestratorService interface {
	SubmitBuild(ctx context.Context, req SubmitBuildInput) (*SubmitBuildResult, error)
	GetBuildStatus(ctx context.Context, buildID string) (*BuildStatusResponse, error)
	CancelBuild(ctx context.Context, buildID string) error
	// GetBuildTenantID returns the tenant that owns a build — used for authorization.
	GetBuildTenantID(ctx context.Context, buildID string) (string, error)
	// GetRedeploySource — Plan J-17 T6.4. Returns the params needed to
	// re-submit a build identical to the source: project_id, environment,
	// commit_sha, and the persisted apps spec. Verifies tenant ownership
	// inside. Returns ErrBuildNotTerminal if the source build is still
	// in flight. Path/Build/Runtime/Start ARE preserved by the apps_spec
	// column (T0.2/T0.1); the legacy `framework` field was removed
	// (F-dom-2 — the build strategy is keyed on `runtime`).
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
	// DeployStatus is how the deploy of this build did (see DeployStatusOf).
	// Absent for a build that has no release in its environment.
	DeployStatus string `json:"deploy_status,omitempty"`
}

// AdmissionChecker is the minimal contract the build handler needs from
// the admission gate. Implemented by *admission.BuildGate. Defined in
// this package to keep routes free of cross-package imports for pure
// interfaces (DIP).
//
// A `nil` AdmissionChecker is legal and means "no admission control" —
// the handler will skip the check entirely, which is the current
// production default until LOOP_RUNTIME_ADMISSION_CONTROL is flipped.
type AdmissionChecker interface {
	// Check returns (admitted, retryAfter, code, message). admitted=true
	// means the build can proceed. When false, the handler returns a
	// 429 with the given code + message + Retry-After header.
	Check(ctx context.Context, cellID, tenantID string) (admitted bool, retryAfter time.Duration, code, message string, err error)
}

// CellResolver resolves the cell_id for a given tenant.
// MEET-053 A-06: Replaces hardcoded DefaultCellID in admission checks.
// Implemented by store.PostgresCellStore.GetTenantCellID.
type CellResolver interface {
	GetTenantCellID(ctx context.Context, tenantID string) (string, error)
}

// BuildHandler provides HTTP handlers for build operations.
//
// C-01 fix: The callback handler has been removed. Build completion is now
// detected by the BuildReconcileLoop which polls Argo Workflow CRDs from the
// control plane. The build cluster no longer calls the API.
type BuildHandler struct {
	orch         BuildOrchestratorService
	logger       *slog.Logger
	admission    AdmissionChecker   // nil = disabled, no-op
	cellResolver CellResolver       // MEET-053: resolves tenant → cell_id per-request
	envEnsurer   EnvironmentEnsurer // Plan T2.2 D5: auto-create env on first deploy --env <novo>
	secretResync SecretResyncer     // V9 L20: resync PG secrets to workload K8s post-envEnsurer
	// releases is optional: nil leaves deploy_status out.
	releases ReleaseStatusLookup
}

// EnvironmentEnsurer ensures the requested environment exists (auto-create
// on first deploy --env <novo>). Plan T2.2 D5. Implementations should be
// idempotent: existing envs return nil immediately; new envs create the
// Environment CR + namespace + ArgoCD app and wait for phase=Active.
// M2 v1.2 (2026-06-07): `project` propagates end-to-end so the
// canonical 3-segment namespace `loop-runtime-<tenant>-<project>-<env>` is
// computed and the Tenant CR spec.environments entry carries the
// project field. Empty project preserves the legacy 2-segment path
// for production-legacy callers (ADR D2).
type EnvironmentEnsurer interface {
	EnsureEnvironment(ctx context.Context, tenantID, project, envName string) error
}

// NewBuildHandler creates a new BuildHandler.
func NewBuildHandler(orch BuildOrchestratorService, logger *slog.Logger) *BuildHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &BuildHandler{
		orch:   orch,
		logger: logger,
	}
}

// WithAdmission attaches a build admission gate and cell resolver.
// MEET-053 A-06: cellResolver replaces the hardcoded cellID string.
// The resolver is called per-request to get the tenant's actual cell.
//
// Both are required: admission is judged per cell, and without a resolver
// the gate would have no cell to judge — a nil checker or resolver leaves
// admission off.
func (h *BuildHandler) WithAdmission(a AdmissionChecker, resolver CellResolver) *BuildHandler {
	if a == nil || resolver == nil {
		return h
	}
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
// The handler decodes and validates the whole request before any side
// effect, prepares the environment, consults the admission gate, then
// submits: each step is a method below.
func (h *BuildHandler) Create(w http.ResponseWriter, r *http.Request) {
	// C-02: Enforce request deadline so handlers don't hang indefinitely.
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrUnauthorized)
		return
	}

	req, err := h.decodeBuildRequest(ctx, r, uc.TenantID)
	// B-059: declare the resource id for the audit row as soon as it is
	// known — a rejected mutation must still be attributable.
	auditctx.SetResourceID(ctx, req.ProjectID)
	if err != nil {
		mapBuildError(w, r, err)
		return
	}

	if terr := h.prepareEnvironment(ctx, uc.TenantID, req); terr != nil {
		httpio.WriteLoopRuntimeError(w, terr)
		return
	}
	if refusal := h.admit(ctx, uc.TenantID); refusal != nil {
		w.Header().Set("Retry-After", strconv.Itoa(refusal.retryAfterSeconds))
		httpio.WriteLoopRuntimeError(w, refusal.err)
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
		mapBuildError(w, r, err)
		return
	}
	httpio.WriteJSON(w, http.StatusAccepted, resp)
}

// decodeBuildRequest decodes the body, resolves a redeploy into its source
// build's parameters, and validates everything — at the system boundary,
// before any environment is provisioned. A returned error is either a typed
// *LoopRuntimeError or an orchestrator error; mapBuildError renders both.
func (h *BuildHandler) decodeBuildRequest(ctx context.Context, r *http.Request, tenantID string) (createBuildRequest, error) {
	var req createBuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, loopruntimeerrors.ErrInvalidRequest
	}
	// Plan J-17 T6.4 — redeploy_of copies project, environment, commit and
	// apps from the source build (tenant ownership and terminal status are
	// verified by the orchestrator). NoCache stays from the request.
	if req.RedeployOf != "" {
		src, err := h.orch.GetRedeploySource(ctx, tenantID, req.RedeployOf)
		if err != nil {
			return req, err
		}
		req.ProjectID, req.Environment, req.CommitSHA, req.Apps = src.ProjectID, src.Environment, src.CommitSHA, src.Apps
	}
	if err := validateBuildRequest(req); err != nil {
		return req, err
	}
	return req, nil
}

// validateBuildRequest checks the request shape. App names and paths flow
// into shell steps of the build pipeline, so they are validated here
// (SEC-3 / CODE-001) against injection and traversal.
func validateBuildRequest(req createBuildRequest) *loopruntimeerrors.LoopRuntimeError {
	if req.ProjectID == "" {
		return loopruntimeerrors.NewLoopRuntimeError("MISSING_FIELD", "Project name is required. Check your loopzero.yaml.")
	}
	if req.Environment == "" {
		return loopruntimeerrors.NewLoopRuntimeError("MISSING_FIELD", "Environment is required")
	}
	// T1.3 follow-up (CR-2): an empty apps array would reach the workflow
	// builder's invariant guard as a 500; it is a caller error.
	if len(req.Apps) == 0 {
		detail := "At least one app is required. Check your `.loopzero/services.json` for a `services:` array."
		if req.RedeployOf != "" {
			// Red Team loop-console#126 — a redeploy has no local services.json
			// to edit; name the source build instead.
			detail = fmt.Sprintf("Source build %s has no apps to redeploy. Pick a build that completed with at least one app.", req.RedeployOf)
		}
		return loopruntimeerrors.NewLoopRuntimeError("MISSING_FIELD", detail)
	}
	// DA-10: the one environment-name rule.
	if err := validation.ValidateEnvironmentName(req.Environment); err != nil {
		return loopruntimeerrors.NewLoopRuntimeError("INVALID_ENVIRONMENT", err.Error())
	}
	for _, app := range req.Apps {
		if err := validation.ValidateAppName(app.Name); err != nil {
			return loopruntimeerrors.NewLoopRuntimeError("INVALID_APP_NAME", fmt.Sprintf("Invalid name for app: %s", err.Error()))
		}
		if err := validation.ValidateAppPath(app.Path); err != nil {
			return loopruntimeerrors.NewLoopRuntimeError("INVALID_APP_PATH", fmt.Sprintf("Invalid path for app %q: %s", app.Name, err.Error()))
		}
	}
	return nil
}

// prepareEnvironment makes the target environment deployable. Plan T2.2 D5:
// a first deploy to a new environment creates it (Environment CR, namespace,
// application) — a no-op for an existing one. V9 L20: secrets set before the
// namespace existed are then resynced; a resync failure is logged, not fatal,
// because a missing secret surfaces as a deploy failure the user can act on.
func (h *BuildHandler) prepareEnvironment(ctx context.Context, tenantID string, req createBuildRequest) *loopruntimeerrors.LoopRuntimeError {
	if h.envEnsurer != nil {
		if err := h.envEnsurer.EnsureEnvironment(ctx, tenantID, req.ProjectID, req.Environment); err != nil {
			var te *loopruntimeerrors.LoopRuntimeError
			if errors.As(err, &te) {
				return te
			}
			return loopruntimeerrors.NewLoopRuntimeError("ENV_PROVISION_FAILED",
				fmt.Sprintf("Failed to provision environment %q: %s", req.Environment, err.Error()))

		}
	}
	if h.secretResync != nil {
		if err := h.secretResync.ResyncEnv(ctx, tenantID, req.ProjectID, req.Environment); err != nil {
			h.logger.Warn("secret resync failed — pods may fail to start with CreateContainerConfigError",
				"error", err, "tenant_id", tenantID, "project", req.ProjectID, "environment", req.Environment)
		}
	}
	return nil
}

// admissionRefusal is a build the admission gate turned away.
type admissionRefusal struct {
	err               *loopruntimeerrors.LoopRuntimeError
	retryAfterSeconds int
}

// admit consults the admission gate (Roadmap 07) for the tenant's cell. The
// gate is advisory: when the cell cannot be resolved or the gate cannot be
// queried, the build proceeds rather than being judged against the wrong
// cell or blocked by an outage. A refusal answers 429 with a Retry-After of
// at least one second.
func (h *BuildHandler) admit(ctx context.Context, tenantID string) *admissionRefusal {
	if h.admission == nil {
		return nil
	}
	cellID, err := h.cellResolver.GetTenantCellID(ctx, tenantID)
	if err != nil {
		h.logger.Warn("admission skipped — the tenant's cell could not be resolved",
			"error", err, "tenant_id", tenantID)
		return nil
	}
	admitted, retryAfter, code, message, err := h.admission.Check(ctx, cellID, tenantID)
	if err != nil {
		h.logger.Warn("admission gate query failed — failing open",
			"error", err, "tenant_id", tenantID, "cell_id", cellID)
		return nil
	}
	if admitted {
		return nil
	}
	secs := int(retryAfter.Round(time.Second).Seconds())
	if secs < 1 {
		secs = 1
	}
	return &admissionRefusal{err: loopruntimeerrors.NewLoopRuntimeError(code, message), retryAfterSeconds: secs}
}

// Get handles GET /api/v1/builds/:id.
// SEC-4: Validates that the authenticated user's tenant owns the build.
func (h *BuildHandler) Get(w http.ResponseWriter, r *http.Request) {
	// MOD-2: Enforce timeout on read-only handlers to prevent slowloris.
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrUnauthorized)
		return
	}

	buildID := chi.URLParam(r, "id")

	// SEC-4: Verify tenant ownership before returning build data.
	// Returns 404 (not 403) to avoid leaking build existence across tenants.
	buildTenantID, err := h.orch.GetBuildTenantID(ctx, buildID)
	if err != nil {
		mapBuildError(w, r, err)
		return
	}
	if buildTenantID != uc.TenantID {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrBuildNotFound)
		return
	}

	resp, err := h.orch.GetBuildStatus(ctx, buildID)
	if err != nil {
		mapBuildError(w, r, err)
		return
	}
	if h.releases != nil && resp != nil {
		statuses, err := h.releases.LatestReleaseStatuses(ctx, uc.TenantID, []string{buildID})
		if err != nil {
			h.logger.Error("deploy status lookup failed", "build_id", buildID, "tenant_id", uc.TenantID, "error", err)
			httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrInternalError)
			return
		}
		resp.DeployStatus = DeployStatusOf(statuses[buildID])
	}

	httpio.WriteJSON(w, http.StatusOK, resp)
}

// Cancel handles POST /api/v1/builds/:id/cancel.
// Validates auth and tenant ownership, delegates to orchestrator which checks
// terminal state, cancels the workflow, and transitions the build to CANCELLED.
func (h *BuildHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	// C-02: Enforce request deadline so handlers don't hang indefinitely.
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrUnauthorized)
		return
	}

	buildID := chi.URLParam(r, "id")

	// SEC-4: Verify tenant ownership before allowing cancel.
	// Returns 404 (not 403) to avoid leaking build existence across tenants.
	buildTenantID, err := h.orch.GetBuildTenantID(ctx, buildID)
	if err != nil {
		mapBuildError(w, r, err)
		return
	}
	if buildTenantID != uc.TenantID {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrBuildNotFound)
		return
	}

	if err := h.orch.CancelBuild(ctx, buildID); err != nil {
		mapBuildError(w, r, err)
		return
	}

	httpio.WriteJSON(w, http.StatusAccepted, map[string]string{
		"status": "cancelled",
	})
}

// mapBuildError maps orchestrator errors to HTTP responses.
//
// Sprint 6 plan qa-sprint6-cleanup-2026-06-25 T3.1 (closes loop-console#34):
// extends the ladder with build.AsSubmitBuildError dispatch BEFORE the
// fallback so wrap-site discriminated errors from BuildOrchestrator.SubmitBuild
// (workflow / project ensure / DB write / app normalize / unknown) surface
// with reason-specific messages composed with the verbatim cause — instead
// of the generic catchall "Something went wrong" that #34 complained about.
// EC-3 preserved: DEPLOY_IN_PROGRESS short-circuit + typed *LoopRuntimeError
// dispatch order unchanged.
// Sprint 8 plan qa-sprint8-cleanup-2026-06-25 T1.1: signature takes the
// *http.Request so the SubmitBuildError branch can thread the request's
// correlation ID into the user-facing message (trace-id fallback). Other
// branches (typed *LoopRuntimeError + generic catchall) ignore r.
func mapBuildError(w http.ResponseWriter, r *http.Request, err error) {
	var loopRuntimeErr *loopruntimeerrors.LoopRuntimeError
	if errors.As(err, &loopRuntimeErr) {
		// DA-18: Override message for DEPLOY_IN_PROGRESS
		if errors.Is(err, loopruntimeerrors.ErrDeployInProgress) {
			httpio.WriteLoopRuntimeError(w, loopruntimeerrors.NewLoopRuntimeError(
				"DEPLOY_IN_PROGRESS",
				"A deploy is already in progress for this environment."))
			return
		}
		httpio.WriteLoopRuntimeError(w, loopRuntimeErr)
		return
	}

	// Sprint 6 ADR D3: dispatch wrap-site discriminated *SubmitBuildError
	// BEFORE the generic catchall. The reason label is the wire form
	// (Stringer output); the verbatim cause is composed into the user-
	// facing message via SubmitBuildError.UserError.
	if se, ok := builds.AsSubmitBuildError(err); ok {
		slog.Error("build trigger failed (typed)",
			"reason", se.Reason.String(),
			"stage", string(se.Stage),
			"detail", se.Detail,
			"cause", se.Cause.Error(),
		)
		// Sprint 8 plan qa-sprint8-cleanup-2026-06-25 T1.1: thread the
		// correlation/trace ID into the user-facing message so when the
		// scrub collapses Detail to placeholder-only, the operator still
		// gets a "Reference: <id>. Contact support if it persists." handle
		// to the server-side log (closes the #35 Red Team original
		// suggestion of separating client-facing from server-side cause).
		httpio.WriteLoopRuntimeError(w, se.UserError(correlation.Read(r)))
		return
	}

	// F-03: Log unexpected errors for debugging — the audit middleware only logs
	// status=500 at INFO level, which is useless for diagnosing root causes.
	slog.Error("unexpected error in build handler",
		"error", err.Error(),
		"type", fmt.Sprintf("%T", err),
	)

	// Fallback for genuinely-unexpected errors (request decode, etc.).
	// Sprint 6 narrowed the orchestrator surface so this only fires for
	// errors that genuinely came from outside BuildOrchestrator.SubmitBuild.
	httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrInternalError)
}
