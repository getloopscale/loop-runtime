// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package builds serves the build endpoints under /api/v1/builds: submitting a
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
package builds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/getloopscale/loopzero/api/internal/routes/httpio"

	"github.com/getloopscale/loopzero/api/internal/services/build"

	"github.com/go-chi/chi/v5"

	"github.com/getloopscale/loopzero/api/internal/correlation"
	auditctx "github.com/getloopscale/loopzero/api/internal/middleware/audit"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"github.com/getloopscale/loopzero/api/internal/validation"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
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
	Name      string   `json:"name"`
	Type      string   `json:"type,omitempty"` // "server", "worker", "frontend"
	Port      int      `json:"port,omitempty"`
	Path      string   `json:"path,omitempty"`
	Runtime   string   `json:"runtime,omitempty"`   // services.json runtime: "python" | "node" — the build-strategy key (T1, F-dom-2)
	Build     string   `json:"build,omitempty"`     // "dockerfile", "buildpack", "custom"
	Start     string   `json:"start,omitempty"`     // T0.2: declared services.json start command (canonical Dockerfile CMD)
	Rendering string   `json:"rendering,omitempty"` // "static" or "server" (Sprint 10)
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
}

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

// NewBuildHandler creates a new BuildHandler.
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
func (h *BuildHandler) Create(w http.ResponseWriter, r *http.Request) {
	// C-02: Enforce request deadline so handlers don't hang indefinitely.
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	var req createBuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInvalidRequest)
		return
	}

	// Plan J-17 T6.4 — redeploy_of short-circuit. Look up the source
	// build, verify tenant + terminal status, copy its params. After
	// this the rest of the handler treats the request as a normal
	// build submission (validation, env ensure, secret resync, etc.).
	if req.RedeployOf != "" {
		src, srcErr := h.orch.GetRedeploySource(ctx, uc.TenantID, req.RedeployOf)
		if srcErr != nil {
			mapBuildError(w, r, srcErr)
			return
		}
		req.ProjectID = src.ProjectID
		req.Environment = src.Environment
		req.CommitSHA = src.CommitSHA
		req.Apps = src.Apps
		// NoCache opt-in stays from the request; everything else
		// comes from the source.
	}

	// Validate required fields
	if req.ProjectID == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"MISSING_FIELD",
			"Project name is required. Check your loopzero.yaml.",
			400,
		))
		return
	}
	if req.Environment == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"MISSING_FIELD",
			"Environment is required",
			400,
		))
		return
	}

	// B-059: declare the resource id for the audit row. The middleware used
	// to buffer this body and scan it for `project_id` to find the same
	// value; the handler already decoded it, so it says so.
	auditctx.SetResourceID(ctx, req.ProjectID)
	// T1.3 follow-up (CR-2 regression caught during cleanroom Final Phase
	// 2026-05-29): an empty `apps` array used to be silently accepted —
	// SubmitBuild would create a Build row, build an empty WorkflowSpec, and
	// the legacy spec.Apps fallback in workflow_builder would emit a Workflow
	// CR with zero steps that Argo "succeeded" trivially. T1.3 (commit
	// 2b9f8040) removed the fallback and added an invariant guard, exposing
	// the gap — empty bodies now reach the guard as 500 INTERNAL_ERROR.
	// Fail fast at the system boundary with a typed 400 instead.
	if len(req.Apps) == 0 {
		// Red Team loopzero-cloud#126 — when this is a redeploy_of request and
		// the source build itself has no apps (legacy seed data or synthetic
		// fixture), the user has no local `.loopzero/services.json` to edit;
		// pointing them there is misleading. Surface a redeploy-specific
		// message that names the source build and explains the actionable
		// fix: pick a source build that has apps.
		detail := "At least one app is required. Check your `.loopzero/services.json` for a `services:` array."
		if req.RedeployOf != "" {
			detail = fmt.Sprintf(
				"Source build %s has no apps to redeploy. Pick a build that completed with at least one app.",
				req.RedeployOf,
			)
		}
		httpio.WriteLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"MISSING_FIELD",
			// Red Team loopzero-cloud#123 — when this is a fresh deploy, guide
			// the customer to the canonical `.loopzero/services.json` (loopzerokit
			// v2). The legacy `loopzero.yaml` / `apps:` has been retired;
			// following stale guidance leads users to edit a file the build
			// silently ignores.
			detail,
			400,
		))
		return
	}

	// Environment validation (DA-10): lowercase only
	if !environmentRegex.MatchString(req.Environment) {
		httpio.WriteLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"INVALID_ENVIRONMENT",
			"Environment names must be lowercase.",
			400,
		))
		return
	}

	// Plan T2.2 D5 — auto-create environment on first deploy --env <novo>.
	// Closes F-S1-02 where LoopZeroApplication CR was created but Environment CR
	// + workload namespace + ArgoCD App+AppProject were not, causing eternal
	// SYNC_FAILED. When envEnsurer is wired, calling EnsureEnvironment is a
	// no-op for existing envs and creates+waits for new ones.
	if h.envEnsurer != nil {
		// M2 v1.2 T3.2: propagate req.ProjectID so the operator
		// computes the canonical `loopzero-<tenant>-<project>-<env>`
		// namespace and writes the project field to spec.environments.
		// req.ProjectID is already validated non-empty upstream.
		if ensureErr := h.envEnsurer.EnsureEnvironment(ctx, uc.TenantID, req.ProjectID, req.Environment); ensureErr != nil {
			// Wrap non-LoopZeroError errors so the handler emits a clean response.
			var te *loopzeroerrors.LoopZeroError
			if errors.As(ensureErr, &te) {
				httpio.WriteLoopZeroError(w, te)
			} else {
				httpio.WriteLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
					"ENV_PROVISION_FAILED",
					fmt.Sprintf("Failed to provision environment %q: %s", req.Environment, ensureErr.Error()),
					500,
				))
			}
			return
		}
	}

	// V9 L20: Resync PG secrets to workload K8s ExternalSecrets. Closes
	// the race where `loopzero secret set` ran BEFORE the namespace existed
	// (first deploy --env <novo> creates the namespace, but the secret
	// POST K8s sync at line ~234 of secrets.go silently failed with
	// "namespaces ... not found"). Pod start would later fail with
	// CreateContainerConfigError because the K8s Secret was never
	// created on workload.
	//
	// Best-effort: log resync errors but don't fail the deploy — the
	// pod-start failure will surface as a deploy timeout if any required
	// secret is missing. Per-secret sync errors are also non-fatal in
	// the resyncer (e.g., partial-success when only one secret has a
	// transient issue).
	if h.secretResync != nil {
		if resyncErr := h.secretResync.ResyncEnv(ctx, uc.TenantID, req.ProjectID, req.Environment); resyncErr != nil {
			slog.Warn("secret resync failed — pods may fail to start with CreateContainerConfigError",
				"error", resyncErr,
				"tenant_id", uc.TenantID,
				"project", req.ProjectID,
				"environment", req.Environment,
			)
		}
	}

	// SEC-3 fix: Validate app paths before they reach shell scripts in the build pipeline.
	// Prevents path traversal and command injection via crafted app.path values.
	// CODE-001 fix: Also validate app names — they flow into shell scripts in
	// loopzeroPacksDockerfileStep() and buildctl arguments.
	for _, app := range req.Apps {
		if err := validation.ValidateAppName(app.Name); err != nil {
			httpio.WriteLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
				"INVALID_APP_NAME",
				fmt.Sprintf("Invalid name for app: %s", err.Error()),
				400,
			))
			return
		}
		if err := validation.ValidateAppPath(app.Path); err != nil {
			httpio.WriteLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
				"INVALID_APP_PATH",
				fmt.Sprintf("Invalid path for app %q: %s", app.Name, err.Error()),
				400,
			))
			return
		}
	}

	// Roadmap 07 — admission gate. Only consulted when configured
	// (WithAdmission + LOOPZERO_ADMISSION_CONTROL=true). A nil checker
	// means the feature is off, which is the pre-roadmap default.
	if h.admission != nil {
		// MEET-053 A-06: Resolve cell_id per-request from tenant's placement.
		cellID := "cell-0" // fallback if resolver not set
		if h.cellResolver != nil {
			if resolved, resolveErr := h.cellResolver.GetTenantCellID(ctx, uc.TenantID); resolveErr == nil {
				cellID = resolved
			} else if h.logger != nil {
				h.logger.Warn("cell resolver failed — using default",
					"error", resolveErr,
					"tenant_id", uc.TenantID,
				)
			}
		}
		admitted, retryAfter, code, message, adErr := h.admission.Check(ctx, cellID, uc.TenantID)
		if adErr != nil {
			// Fail-open: the gate is advisory, a query error must not
			// penalize users by blocking builds. Log and continue.
			if h.logger != nil {
				h.logger.Warn("admission gate query failed — failing open",
					"error", adErr,
					"tenant_id", uc.TenantID,
					"cell_id", cellID,
				)
			}
		} else if !admitted {
			// Roadmap 07 DoD: 429 with Retry-After header.
			secs := int(retryAfter.Round(time.Second).Seconds())
			if secs < 1 {
				secs = 1
			}
			w.Header().Set("Retry-After", fmt.Sprintf("%d", secs))
			httpio.WriteLoopZeroError(w, loopzeroerrors.NewLoopZeroError(code, message, 429))
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
		CorrelationID: correlation.Read(r), //nolint:contextcheck // header from original request (T6.1 uniform read via helper)
	})
	if err != nil {
		mapBuildError(w, r, err)
		return
	}

	httpio.WriteJSON(w, http.StatusAccepted, resp)
}

// Get handles GET /api/v1/builds/:id.
// SEC-4: Validates that the authenticated user's tenant owns the build.
func (h *BuildHandler) Get(w http.ResponseWriter, r *http.Request) {
	// MOD-2: Enforce timeout on read-only handlers to prevent slowloris.
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
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
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrBuildNotFound)
		return
	}

	resp, err := h.orch.GetBuildStatus(ctx, buildID)
	if err != nil {
		mapBuildError(w, r, err)
		return
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
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
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
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrBuildNotFound)
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
// Sprint 6 plan qa-sprint6-cleanup-2026-06-25 T3.1 (closes loopzero-cloud#34):
// extends the ladder with build.AsSubmitBuildError dispatch BEFORE the
// fallback so wrap-site discriminated errors from BuildOrchestrator.SubmitBuild
// (workflow / project ensure / DB write / app normalize / unknown) surface
// with reason-specific messages composed with the verbatim cause — instead
// of the generic catchall "Something went wrong" that #34 complained about.
// EC-3 preserved: DEPLOY_IN_PROGRESS short-circuit + typed *LoopZeroError
// dispatch order unchanged.
// Sprint 8 plan qa-sprint8-cleanup-2026-06-25 T1.1: signature takes the
// *http.Request so the SubmitBuildError branch can thread the request's
// correlation ID into the user-facing message (trace-id fallback). Other
// branches (typed *LoopZeroError + generic catchall) ignore r.
func mapBuildError(w http.ResponseWriter, r *http.Request, err error) {
	var loopzeroErr *loopzeroerrors.LoopZeroError
	if errors.As(err, &loopzeroErr) {
		// DA-18: Override message for DEPLOY_IN_PROGRESS
		if errors.Is(err, loopzeroerrors.ErrDeployInProgress) {
			httpio.WriteLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
				"DEPLOY_IN_PROGRESS",
				"A deploy is already in progress for this environment.",
				409,
			))
			return
		}
		httpio.WriteLoopZeroError(w, loopzeroErr)
		return
	}

	// Sprint 6 ADR D3: dispatch wrap-site discriminated *SubmitBuildError
	// BEFORE the generic catchall. The reason label is the wire form
	// (Stringer output); the verbatim cause is composed into the user-
	// facing message via SubmitBuildError.UserError.
	if se, ok := build.AsSubmitBuildError(err); ok {
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
		httpio.WriteLoopZeroError(w, se.UserError(correlation.Read(r)))
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
	httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
}
