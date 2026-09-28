// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package projectshttp serves the project endpoints under /api/v1/projects: create,
// list, read and delete, plus the tenant-CR side effects a project carries.
//
// Sixteenth family to leave the flat `routes` package (B-080, slice-36).
//
// Its extraction is the one that forced the `pagination` leaf. Three helpers it
// shared with `environments` — and one it took from the parent — had been
// invisible while everything lived in one package; splitting the two families
// apart is what made the compiler refuse them.
package projectshttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/getloopscale/loopzero/api/internal/projects"
	"github.com/getloopscale/loopzero/api/internal/validation"
	"log/slog"
	"net/http"
	"time"

	"github.com/getloopscale/loopzero/api/internal/httpio"
	"github.com/getloopscale/loopzero/api/internal/pagination"
	"github.com/getloopscale/loopzero/api/internal/plantier"
	pkgdomain "github.com/getloopscale/loopzero/pkg/domain"

	"github.com/go-chi/chi/v5"

	"github.com/getloopscale/loopzero/api/internal/correlation"
	auditctx "github.com/getloopscale/loopzero/api/internal/middleware/audit"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"github.com/getloopscale/loopzero/api/internal/ports"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

var (
	// ErrProjectExists is an alias for ports.ErrProjectExists (backward compatibility).
	ErrProjectExists = ports.ErrProjectExists
)

// Type aliases for backward compatibility -- canonical definitions are in domain/.
type (
	CreateProjectInput  = ports.CreateProjectInput
	ProjectResult       = ports.ProjectResult
	ProjectSummary      = ports.ProjectSummary
	ProjectStore        = ports.ProjectStore
	TenantCRSpec        = ports.TenantCRSpec
	TenantCRManager     = ports.TenantCRManager
	ProjectDeleteInput  = projects.ProjectDeleteInput
	ProjectDeleteOutput = projects.ProjectDeleteOutput
	ProjectDeleter      = projects.Deleter
)

// EnvironmentEnsurer creates the environment a new project needs.
//
// Declared HERE rather than imported from the family that also declares it: a Go
// interface is structural, so the consumer naming exactly the method it calls is
// both idiomatic and the DIP shape the architecture rules ask for. Importing the
// sibling's copy would be the family→family edge the routes fence forbids, and
// the fence is right — the dependency the handler actually has is on ONE method,
// not on a package (B-080, slice-36).
type EnvironmentEnsurer interface {
	EnsureEnvironment(ctx context.Context, tenantID, project, envName string) error
}

// ProjectHandler provides HTTP handlers for project operations.
type ProjectHandler struct {
	store         ports.ProjectStore
	crMgr         TenantCRManager
	deleter       ProjectDeleter
	envEnsurer    EnvironmentEnsurer // T4.1: auto-create production env on POST /api/v1/projects
	cellResolver  TenantCellResolver
	planTierStore pkgdomain.PlanTierStore // T2.3.b: resolves tenant's canonical plan instead of hardcoded "starter"
}

// NewProjectHandler creates a new ProjectHandler.
func NewProjectHandler(store ports.ProjectStore, crMgr TenantCRManager) *ProjectHandler {
	return &ProjectHandler{store: store, crMgr: crMgr}
}

// SetDeleter configures the project deleter. Called from main.go after all deps are ready.
func (h *ProjectHandler) SetDeleter(d ProjectDeleter) {
	h.deleter = d
}

// SetPlanTierStore wires the PlanTierStore used by Create to resolve the
// tenant's canonical plan before EnsureTenant. T2.3.b of plan
// 2026-06-11-code-review-remediation v1.2 — closes H7 CRITICAL silent
// downgrade by replacing the literal `Plan: "starter"` with a real DB
// lookup via pkg/ports.ResolvePlanTier (ADR D4).
//
// Production main.go MUST call this with the canonical store. Tests
// that don't set it fall back to PlanTierStarter (matching pre-T2.3.b
// behavior so the wider test suite keeps passing during transition).
func (h *ProjectHandler) SetPlanTierStore(s pkgdomain.PlanTierStore) {
	h.planTierStore = s
}

// TenantCellResolver reads the cell the engine placed a tenant on (tenants.cell_id).
type TenantCellResolver interface {
	GetTenantCellID(ctx context.Context, tenantID string) (string, error)
}

// SetTenantCellResolver wires the lookup Create uses so the Tenant CR it creates
// names the tenant's placed cell instead of the CRD default (#412).
func (h *ProjectHandler) SetTenantCellResolver(r TenantCellResolver) {
	h.cellResolver = r
}

// SetEnvEnsurer wires the EnvironmentEnsurer used by Create to auto-create
// the canonical `production` environment for every new project per ADR D4
// of plan 2026-06-09-m2-operator-canonical-fix v1.4 (Vercel-style: every
// project has production from day 1). Called from main.go after all deps
// are ready. When unset (e.g., unit tests that only exercise the project
// store path), Create skips the env-auto-create step and behaves identically
// to the pre-T4.1 handler — preserves backwards-compatibility with the
// existing happy-path test suite.
func (h *ProjectHandler) SetEnvEnsurer(e EnvironmentEnsurer) {
	h.envEnsurer = e
}

// List handles GET /api/v1/projects.
// Returns all projects for the authenticated tenant.
func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	// Plan 2026-06-08-dashboard-screens-review-fixes T2.1 + ADR D2:
	// switched to the summary variant so the dashboard /projects table
	// renders M2-accurate "Live envs" count + status-derived StatusDot
	// without an N+1 client-side fanout. Response is wire-format
	// additive — legacy consumers (CLI `loopzero project list`) ignore the
	// extra fields per Rule 5.
	projects, err := h.store.ListByTenantWithSummary(ctx, uc.TenantID)
	if err != nil {
		slog.Error("failed to list projects with summary",
			"tenant_id", uc.TenantID,
			"error", err.Error(),
		)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	// Plan 2026-06-08-dashboard-screens-review-fixes T2.3 + ADR D8:
	// pagination scaffold via Go-side slicing. limit defaults to 50,
	// offset defaults to 0, limit hard-capped at 100 (defense vs
	// scan-of-tenant DoS). Tenant ceiling (Team plan = 10 projects ×
	// 20 envs = 200 envs aggregate) makes SQL-side LIMIT/OFFSET overkill;
	// Go-side slicing is honest pagination for current scale. Defer
	// cursor-based pagination to v1.x (plan §ADR D8 "Cursor rejected").
	// `total` field carries the UNPAGINATED count so the dashboard can
	// render "Showing N of M" + "Load more" affordances (J-17 lesson).
	total := len(projects)
	limit, offset := pagination.ParsePaginationParams(r)
	from, to := pagination.SliceWindow(total, limit, offset)
	httpio.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"projects": projects[from:to],
		"total":    total,
	})
}

// Get handles GET /api/v1/projects/{name}.
// Returns a single project by name with the same enriched summary shape as
// the list endpoint (env_count + apps_count + last_deploy_status +
// latest_framework). T6.1 of plan 2026-06-08-pre-release-test-rigor: detail
// endpoint MUST be by-construction consistent with list endpoint so the
// dashboard's project-detail page renders the same metrics that drove the
// list view. Implemented as ListByTenantWithSummary + in-memory filter (KISS,
// reuses the single optimized SQL round-trip; O(n) over a tenant's project
// count — microseconds for typical ≤100 projects/tenant).
func (h *ProjectHandler) Get(w http.ResponseWriter, r *http.Request) {
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

	summaries, err := h.store.ListByTenantWithSummary(ctx, uc.TenantID)
	if err != nil {
		slog.Error("failed to list projects with summary",
			"tenant_id", uc.TenantID,
			"project", name,
			"error", err.Error(),
		)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	for i := range summaries {
		if summaries[i].Name == name {
			httpio.WriteJSON(w, http.StatusOK, summaries[i])
			return
		}
	}
	httpio.WriteLoopZeroError(w, loopzeroerrors.NewProjectNotFound(name))
}

// createProjectRequest is the JSON request body for POST /api/v1/projects.
type createProjectRequest struct {
	Name string                   `json:"name"`
	Apps []map[string]interface{} `json:"apps,omitempty"`
}

// Create handles POST /api/v1/projects.
func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInvalidRequest)
		return
	}

	// Validate name (RFC 1123)
	if !validation.IsRFC1123Name(req.Name) {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInvalidName.Wrap("project name must be lowercase, alphanumeric, and may contain hyphens"))
		return
	}

	// B-059: declare the resource id for the audit row. project.deleted
	// carries the name from its URL; project.created carried nothing.
	auditctx.SetResourceID(ctx, req.Name)

	// Create project in DB
	result, err := h.store.CreateProject(ctx, CreateProjectInput{
		TenantID:  uc.TenantID,
		Name:      req.Name,
		CreatedBy: uc.UserID,
	})
	if err != nil {
		if errors.Is(err, ErrProjectExists) {
			httpio.WriteLoopZeroError(w, loopzeroerrors.ErrProjectExists)
			return
		}
		// M4: a typed *LoopZeroError from the store (e.g. PROJECTS_EXCEED_PLAN → 422)
		// must surface as-is, not be collapsed to a 500. The store already counted
		// the cap atomically + incremented the violation metric.
		var te *loopzeroerrors.LoopZeroError
		if errors.As(err, &te) {
			httpio.WriteLoopZeroError(w, te)
			return
		}
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	// Create/update Tenant CR in Kubernetes — this triggers the TenantController
	// which provisions Gitea repos, ArgoCD apps, namespaces, RBAC, and NetworkPolicies.
	// ADR-0006 Invariant D: fail loud at ownership boundaries — silent failure here
	// causes cascading deploy failures (namespace not found, ArgoCD app missing).
	//
	// T2.3.b of plan 2026-06-11-code-review-remediation v1.2: resolve the
	// tenant's canonical Plan via pkg/ports.ResolvePlanTier (ADR D4)
	// instead of hardcoding "starter". Pre-T2.3.b code had:
	//     EnsureTenant(ctx, uc.TenantID, TenantCRSpec{Plan: "starter"})
	// which silently downgraded paying Pro/Team tenants on reconnect (H7
	// CRITICAL from /loop-code-review).
	//
	// Failure modes (per ADR D4 contract):
	//   - planTierStore nil          → transition fallback "starter"
	//                                  (production main.go MUST wire it)
	//   - ResolvePlanTier ErrUnknown → 503 PLATFORM_DEGRADED
	//                                  (NEVER silent downgrade)
	//   - any other store error      → 503 PLATFORM_DEGRADED
	planTier, terr := plantier.Resolve(ctx, h.planTierStore, uc.TenantID)
	if terr != nil {
		httpio.WriteLoopZeroError(w, terr)
		return
	}
	planValue := planTier.String()
	cellID, cerr := h.tenantCell(ctx, uc.TenantID)
	if cerr != nil {
		slog.Error("failed to resolve the tenant's cell", "tenant_id", uc.TenantID, "error", cerr.Error())
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrPlatformDegraded.Wrap(
			fmt.Sprintf("tenant placement unavailable for %s — retry or contact support", uc.TenantID),
		))
		return
	}
	if err := h.crMgr.EnsureTenant(ctx, uc.TenantID, TenantCRSpec{Plan: planValue, CellID: cellID}); err != nil {
		slog.Error("failed to ensure tenant CR",
			"tenant_id", uc.TenantID,
			"error", err.Error(),
		)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrPlatformDegraded.Wrap(
			fmt.Sprintf("tenant infrastructure provisioning failed for %s — retry or contact support", uc.TenantID),
		))
		return
	}

	// T4.1 of plan 2026-06-09-m2-operator-canonical-fix v1.4 (ADR D4 —
	// Vercel-style "every project has production from day 1"): auto-create
	// the canonical `production` environment immediately after the project
	// row + Tenant CR are in place. Without this, the user has to manually
	// create the env via the dashboard before the first deploy works —
	// the LIVE friction documented in
	// feedback_m2_canonical_drift_operator_renderer.
	//
	// The EnvironmentEnsurer writes the `environments` PG row + appends to
	// Tenant.Spec.Environments (TenantEnvironmentEntry.Project=req.Name,
	// Name="production"). TenantController then cascades into Environment
	// CR creation; EnvironmentController.reconcileNamespace (T3.1) computes
	// the canonical M2 namespace `loopzero-<tenant>-<project>-production`.
	//
	// EC-5 / R8 honest disclosure: the strict PG-tx-first/K8s-after
	// ordering described in the plan TDD body (lines 569-579) would
	// require a Store-level transaction refactor (BeginTx → INSERT
	// projects → INSERT environments → K8s call → COMMIT-or-ROLLBACK).
	// The current Store API exposes CreateProject as a single
	// auto-committed operation, so a K8s failure leaves the project row
	// committed. Per Unbreakable Rule 3 this is documented as a known
	// follow-up: tracked in the implementation contract known_followups
	// list. The functional contract (production env exists after a
	// successful POST) is preserved; the rollback path is deferred.
	// #124 (qa-dogfood-cleanup-2026-06-21 T4.2 / ADR-Q5): seed BOTH
	// canonical permanent environments — production AND preview — at
	// project.created. The legacy single-env seed (production only)
	// surfaced as a UI inconsistency (Environments view listed only
	// production while the model + QA doc J-10 + add-secret dialog
	// (#127) all expect both to be PaaS-permanent). Iteration order
	// is fixed by pkgdomain.PermanentEnvs so the dashboard renders
	// deterministically. Partial-failure note: if production seeds
	// OK but preview fails, the project remains usable (production
	// is the legacy-compat target); the preview row can be backfilled
	// via the migration shipped in T4.2 of the plan. Preview seeding
	// is non-fatal because the Vercel/Netlify model treats preview as
	// derived from PR existence, so an absent preview is recoverable.
	if h.envEnsurer != nil {
		for _, envName := range pkgdomain.PermanentEnvs {
			if err := h.envEnsurer.EnsureEnvironment(ctx, uc.TenantID, req.Name, envName); err != nil {
				// Production failure is fatal; preview failure is logged
				// + tolerated (deferred backfill via migration).
				if envName == pkgdomain.DefaultPermanentEnv {
					slog.Error("failed to auto-create production environment",
						"tenant_id", uc.TenantID,
						"project", req.Name,
						"error", err.Error(),
					)
					httpio.WriteLoopZeroError(w, loopzeroerrors.ErrPlatformDegraded.Wrap(
						fmt.Sprintf("production environment auto-create failed for project %q — retry or contact support", req.Name),
					))
					return
				}
				slog.Warn("preview permanent env seed failed — project remains usable; backfill on next project.update or via migration",
					"tenant_id", uc.TenantID,
					"project", req.Name,
					"env", envName,
					"error", err.Error(),
				)
			}
		}
	}

	httpio.WriteJSON(w, http.StatusCreated, result)
}

// Delete handles DELETE /api/v1/projects/{name}.
// Deletes a project from all environments.
func (h *ProjectHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 300*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	if h.deleter == nil {
		slog.Error("project delete handler called but deleter is not configured")
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("project name is required"))
		return
	}

	// Look up project by name
	project, err := h.store.GetByName(ctx, uc.TenantID, name)
	if err != nil {
		slog.Error("project lookup failed", "project", name, "error", err)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if project == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.NewProjectNotFound(name))
		return
	}

	correlationID := correlation.Read(r) // T6.1 uniform read via helper

	result, err := h.deleter.Delete(ctx, ProjectDeleteInput{
		TenantID:      uc.TenantID,
		ProjectID:     project.ID,
		ProjectName:   name,
		Environment:   "", // all environments
		DeletedBy:     uc.UserID,
		CorrelationID: correlationID,
	})
	if err != nil {
		slog.Error("project delete failed", "project", name, "error", err)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	httpio.WriteJSON(w, http.StatusOK, result)
}

// DeleteEnvironment handles DELETE /api/v1/projects/{name}/environments/{env}.
// Deletes a project from a specific environment.
func (h *ProjectHandler) DeleteEnvironment(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 300*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	if h.deleter == nil {
		slog.Error("project delete environment handler called but deleter is not configured")
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	name := chi.URLParam(r, "name")
	env := chi.URLParam(r, "env")
	if name == "" || env == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("project name and environment are required"))
		return
	}

	// Look up project by name
	project, err := h.store.GetByName(ctx, uc.TenantID, name)
	if err != nil {
		slog.Error("project lookup failed", "project", name, "error", err)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if project == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.NewProjectNotFound(name))
		return
	}

	correlationID := correlation.Read(r) // T6.1 uniform read via helper

	result, err := h.deleter.Delete(ctx, ProjectDeleteInput{
		TenantID:      uc.TenantID,
		ProjectID:     project.ID,
		ProjectName:   name,
		Environment:   env,
		DeletedBy:     uc.UserID,
		CorrelationID: correlationID,
	})
	if err != nil {
		slog.Error("project delete failed", "project", name, "env", env, "error", err)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	httpio.WriteJSON(w, http.StatusOK, result)
}

// tenantCell returns the tenant's placed cell, or "" when no resolver is wired (the
// Tenant CR then takes the CRD default, the pre-#412 behaviour).
func (h *ProjectHandler) tenantCell(ctx context.Context, tenantID string) (string, error) {
	if h.cellResolver == nil {
		return "", nil
	}
	return h.cellResolver.GetTenantCellID(ctx, tenantID)
}
