// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package environments serves the environment endpoints under
// /api/v1/environments: the per-project deploy targets and their readiness.
//
// Seventeenth family to leave the flat `routes` package (B-080, slice-36),
// extracted alongside `projects` because the two shared four helpers and a pile
// of type aliases — separating one without the other would have moved the
// coupling rather than removed it.
package environments

import (
	"context"
	"encoding/json"
	"errors"

	"fmt"
	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/api/internal/validation"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/api/internal/httpio"
	"github.com/getloopscale/loopzero/api/internal/pagination"

	"github.com/go-chi/chi/v5"

	auditctx "github.com/getloopscale/loopzero/api/internal/middleware/audit"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	pkgdomain "github.com/getloopscale/loopzero/pkg/domain"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// reservedEnvironmentNames contains names that cannot be used for environments.
// M2 v1.2 EC-8: `production` is reserved tenant-wide — user cannot
// create (web, production). The legacy production env (per ADR D2) is
// provisioned by tenant bootstrap, not via this handler.
var reservedEnvironmentNames = map[string]bool{
	"default":                     true,
	"kube-system":                 true,
	"loopzero-system":                 true,
	"argocd":                      true,
	"monitoring":                  true,
	pkgdomain.DefaultPermanentEnv: true, // M2 v1.2 — EC-8 cross-project reserved
}

// envDeleteCleanupWarning is the user-facing warning emitted when the
// deleted environment still has an active release. Reaping is async on
// the operator side; the message uses product language (Rule 3 — no
// "ArgoCD", "Gitea", "MinIO", etc. in user-facing strings).
// Plan: 2026-06-15-code-review-49-findings-remediation T0.1 (R2-F1 BLOCKER).
const envDeleteCleanupWarning = "environment removed but cleanup may take a few seconds; run 'loopzero platform status' to verify"

// T1.3 of deep-review-remediation-plan (closes find_arch_001 + find_dep_routes_type_hub partial):
// canonical types live in domain/. Aliases preserve back-compat with
// handler code in this file.
type (
	EnvironmentEntry  = ports.EnvironmentEntry
	EnvironmentDetail = ports.EnvironmentDetail
	EnvProjectInfo    = ports.EnvProjectInfo
)

// EnvironmentManager abstracts environment operations on the Tenant CRD.
// In production, this is the TenantCRManager. In tests, a fake.
//
// M2 v1.2 (2026-06-07): the write/lookup methods carry an explicit
// `project` argument. The canonical 3-segment namespace
// `loopzero-<tenant>-<project>-<env>` is computed by `pkg/naming` and is
// the source of truth for routing post-M2. Legacy callers (Phase 2/3
// of the M2 plan) may pass `project=""` during the cascade; the
// operator preserves backward compatibility for tuples that landed
// before the M2 migration (ADR D2 — production legacy).
type EnvironmentManager interface {
	ListEnvironments(ctx context.Context, tenantID string) ([]EnvironmentEntry, error)
	AddEnvironment(ctx context.Context, tenantID, project, envName string) error
	RemoveEnvironment(ctx context.Context, tenantID, project, envName string) error
	GetEnvironmentDetail(ctx context.Context, tenantID, project, envName string) (*EnvironmentDetail, error)
}

// EnvKPILookup enriches the environments list with the dashboard KPI
// signals (last_deploy timestamp + git commit SHA) that come from the
// releases table. Implementations resolve "the latest ACTIVE release per
// environment for a tenant". J-10 v1.1 LIVE catch 2026-06-07.
//
// LatestActivePerEnv returns a map keyed by environment name. Missing
// entries mean "no ACTIVE release yet" — the dashboard renders honest "—".
type EnvKPILookup interface {
	LatestActivePerEnv(ctx context.Context, tenantID string) (map[string]EnvKPI, error)
}

// EnvKPI is the subset of release data the dashboard renders. Kept minimal
// to avoid coupling routes to release_wrapper internals.
type EnvKPI struct {
	LastDeploy    string // ISO-8601 timestamp
	LastDeploySha string // git commit SHA (full)
}

// PreviewSlotLookup reads preview slots for a tenant.
//
// 2026-06-07 per-project model (migration 000028): a tenant can have
// N preview slots — one per project. ListPreviewSlots returns all
// rows; the dashboard renders one preview row per project.
type PreviewSlotLookup interface {
	// ListPreviewSlots returns ALL preview slots for a tenant — one
	// row per project that has had a preview at any time. Empty
	// slice means the tenant has no projects with preview history;
	// returning nil is allowed too.
	ListPreviewSlots(ctx context.Context, tenantID string) ([]PreviewSlotInfo, error)
}

// PreviewSlotInfo is the subset of preview_slot the dashboard renders.
// ActivePRNumber == nil means "no active preview" — the dashboard
// renders "No active branch" empty state.
type PreviewSlotInfo struct {
	ProjectID       string  `json:"project_id"`
	ActivePRNumber  *int32  `json:"active_pr_number,omitempty"`
	ActiveBranch    *string `json:"active_branch,omitempty"`
	ActiveReleaseID *string `json:"active_release_id,omitempty"`
	URL             *string `json:"url,omitempty"`
	UpdatedAt       string  `json:"updated_at"`
}

// EnvProjectLookup gates the cross-tenant project validation for the M2
// per-(project, env) API contract. Implementations MUST scope the query
// to (tenant_id, name) — never just by name — otherwise user of tenant A
// can target a project of tenant B by naming it verbatim and create an
// Environment on the wrong namespace (EC-1 of plan v1.2, the
// SEPA-CRITICAL security gate).
//
// Returns (true, nil) when the (tenant, project) tuple exists; (false, nil)
// when it doesn't (404 path); error for I/O failure (500 path).
//
// Nil EnvProjectLookup on the handler is intentionally permitted so
// test fixtures and no-database boots keep working without DB plumbing. Production wiring (api/cmd/main.go)
// MUST inject a real implementation backed by the projects table.
type EnvProjectLookup interface {
	ProjectExistsForTenant(ctx context.Context, tenantID, projectName string) (bool, error)
}

// EnvActiveReleaseLookup surfaces whether an (tenant, project, env)
// triple still has a desired or active release row in env_targets. Used
// by Delete to attach an EC-7 warning ("ArgoCD App may persist briefly")
// so operators are informed about the eventual-consistency cleanup
// instead of being surprised by a stale App in `loopzero platform status`.
//
// Nil implementation = no warning surfaced (test fixtures + legacy).
type EnvActiveReleaseLookup interface {
	HasActiveRelease(ctx context.Context, tenantID, project, envName string) (bool, error)
}

// EnvironmentHandler provides HTTP handlers for environment operations.
type EnvironmentHandler struct {
	envMgr       EnvironmentManager
	kpiLookup    EnvKPILookup           // optional — nil means "skip enrichment" (legacy + tests)
	slotLookup   PreviewSlotLookup      // optional — nil → preview block omitted from List response
	projectStore EnvProjectLookup       // M2: cross-tenant project gate (EC-1)
	relLookup    EnvActiveReleaseLookup // M2: EC-7 orphan-warning lookup
}

// NewEnvironmentHandler creates a new EnvironmentHandler.
func NewEnvironmentHandler(envMgr EnvironmentManager) *EnvironmentHandler {
	return &EnvironmentHandler{envMgr: envMgr}
}

// WithProjectLookup wires the per-tenant project validator. Without it,
// the Create / Delete / Get handlers degrade to legacy mode (skip the
// tenant-scoped project gate — EC-1 protection is bypassed). Production
// callers MUST wire this; tests with hard-coded fixtures may skip.
func (h *EnvironmentHandler) WithProjectLookup(p EnvProjectLookup) *EnvironmentHandler {
	h.projectStore = p
	return h
}

// WithActiveReleaseLookup wires the EC-7 warning emitter for Delete.
// Nil → no warning surfaced; operators rely on `loopzero platform status`
// instead.
func (h *EnvironmentHandler) WithActiveReleaseLookup(l EnvActiveReleaseLookup) *EnvironmentHandler {
	h.relLookup = l
	return h
}

// WithKPILookup wires an optional KPI lookup so List enriches the response
// with release_active-derived signals. Returns the handler for fluent setup.
func (h *EnvironmentHandler) WithKPILookup(lookup EnvKPILookup) *EnvironmentHandler {
	h.kpiLookup = lookup
	return h
}

// WithPreviewSlotLookup wires the single-slot preview source so List
// returns the {permanent[], preview{}} response shape (D7 of plan
// 2026-06-07-preview-as-category-single-slot-plan.md). When nil, the
// response retains the legacy {environments: [...]} shape so existing
// callers don't break — Phase 5 callers MUST wire this for the
// dashboard to render the preview row.
func (h *EnvironmentHandler) WithPreviewSlotLookup(lookup PreviewSlotLookup) *EnvironmentHandler {
	h.slotLookup = lookup
	return h
}

// createEnvironmentRequest is the JSON request body for POST /api/v1/environments.
//
// M2 v1.2 (2026-06-07): `Project` is REQUIRED. Empty value → 400
// INVALID_FIELD. The project is the user-friendly slug (`projects.name`),
// NOT the UUID (`projects.id`) — same convention as `loopzero deploy`
// (EC-3 of plan v1.2; the canonical "Project name vs project ID" rule
// in CLAUDE.md).
type createEnvironmentRequest struct {
	Name    string `json:"name"`
	Project string `json:"project"`
}

// List handles GET /api/v1/environments.
// Returns all environments for the authenticated tenant.
func (h *EnvironmentHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	envs, err := h.envMgr.ListEnvironments(ctx, uc.TenantID)
	if err != nil {
		slog.Error("failed to list environments",
			"tenant_id", uc.TenantID,
			"error", err.Error(),
		)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	// Red Team loopzero-cloud#48 closure — A01:2021 Broken Access Control / CWE-639.
	// `?project=` was silently ignored server-side, letting a caller probe
	// for env names of projects outside their tenant. Canonical fix:
	//   1) Cross-tenant gate via projectStore (same EC-1 SEPA-CRITICAL
	//      pattern as Create) — return 404 PROJECT_NOT_FOUND when the
	//      project doesn't belong to the caller.
	//   2) Filter result set to entries whose Project matches OR the
	//      legacy production entry (empty Project) which is shared across
	//      projects per ADR D2.
	// Empty ?project= preserves the legacy J-10 dashboard shape (all envs).
	if projectFilter := strings.TrimSpace(r.URL.Query().Get("project")); projectFilter != "" {
		if h.projectStore != nil {
			exists, perr := h.projectStore.ProjectExistsForTenant(ctx, uc.TenantID, projectFilter)
			if perr != nil {
				slog.Error("failed to validate project filter",
					"tenant_id", uc.TenantID,
					"project", projectFilter,
					"error", perr.Error(),
				)
				httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
				return
			}
			if !exists {
				httpio.WriteLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
					"PROJECT_NOT_FOUND",
					fmt.Sprintf("project %q not found in your account", projectFilter)))
				return
			}
		}
		// Issue #142 — the legacy empty-Project entry is a FALLBACK for an
		// env name, not an addition to it.
		//
		// The original `e.Project == projectFilter || e.Project == ""` was
		// correct when written: before the #124 backfill the legacy row was
		// the ONLY row a tenant had, and dropping it hid a live environment.
		// After the backfill both rows exist for the same name, so the clause
		// that guaranteed visibility started guaranteeing a duplicate —
		// `production, preview, production, preview`.
		//
		// Deleting the legacy clause is NOT the fix: a tenant whose backfill
		// has not run carries only empty-Project rows and would get an empty
		// list. So: take the project-scoped row when it exists, fall back to
		// the legacy row for names that have no scoped row yet. Order of the
		// underlying list is preserved (the CR order is meaningful to the
		// dashboard); only the redundant legacy entries are dropped.
		scoped := make(map[string]bool, len(envs))
		for _, e := range envs {
			if e.Project == projectFilter {
				scoped[e.Name] = true
			}
		}
		filtered := envs[:0]
		for _, e := range envs {
			switch {
			case e.Project == projectFilter:
				filtered = append(filtered, e)
			case e.Project == "" && !scoped[e.Name]:
				// Legacy shared entry (ADR D2), and no backfilled row has
				// superseded it for this name.
				filtered = append(filtered, e)
			}
		}
		envs = filtered
	}

	// J-10 v1.1 LIVE catch 2026-06-07: enrich each entry with KPI signals
	// (last_deploy + last_deploy_sha) when a lookup is wired. Tolerant of
	// errors — KPI enrichment is best-effort, the list itself must always
	// succeed.
	if h.kpiLookup != nil {
		kpi, kpiErr := h.kpiLookup.LatestActivePerEnv(ctx, uc.TenantID)
		if kpiErr != nil {
			// Log but do not fail the list — dashboard renders "—" gracefully.
			slog.Warn("environment KPI enrichment failed",
				"tenant_id", uc.TenantID,
				"error", kpiErr.Error(),
			)
		} else {
			for i := range envs {
				if k, ok := kpi[envs[i].Name]; ok {
					envs[i].LastDeploy = k.LastDeploy
					envs[i].LastDeploySha = k.LastDeploySha
				}
			}
		}
	}

	// 2026-06-07 per-project preview model (migration 000028).
	// Response shape when slot lookup is wired:
	//   {environments[], permanent[], previews: [{project_id, ...}, ...]}
	//
	// `environments` is preserved for back-compat (legacy hooks).
	// `permanent` mirrors `environments` today.
	// `previews` is the new field: one entry per project that has had
	// a preview, with the active_pr_number nullable. Empty list when
	// the tenant has never had a preview.
	if h.slotLookup != nil {
		slots, slotErr := h.slotLookup.ListPreviewSlots(ctx, uc.TenantID)
		if slotErr != nil {
			// Best-effort: log but don't fail the list. The dashboard
			// degrades gracefully (no preview rows) when the slot lookup
			// errors.
			slog.Warn("preview slot lookup failed (dashboard preview rows omitted)",
				"tenant_id", uc.TenantID, "error", slotErr.Error())
			slots = []PreviewSlotInfo{}
		}
		// Pull permanent env names from the same envs list.
		permanent := make([]string, 0, len(envs))
		for _, e := range envs {
			permanent = append(permanent, e.Name)
		}
		// Surface ALL slots, even ones with ActivePRNumber == nil —
		// the dashboard renders "No active branch" empty state per
		// project so users see "project X has no preview right now"
		// instead of the project simply disappearing.
		if slots == nil {
			slots = []PreviewSlotInfo{}
		}
		// T2.3 pagination: slice `environments` for legacy consumers;
		// `permanent`+`previews` are categorical lookups (string names +
		// per-project slot info) so they remain unpaginated. `total`
		// carries the unpaginated environment count for dashboard
		// "Showing N of M" affordance.
		// T2.3 pagination.
		envsTotal := len(envs)
		envsLimit, envsOffset := pagination.ParsePaginationParams(r)
		envsFrom, envsTo := pagination.SliceWindow(envsTotal, envsLimit, envsOffset)
		httpio.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"environments": envs[envsFrom:envsTo], // legacy (back-compat) — paginated post-T2.3
			"permanent":    permanent,
			"previews":     slots,
			"total":        envsTotal,
		})
		return
	}

	envsTotal := len(envs)
	envsLimit, envsOffset := pagination.ParsePaginationParams(r)
	envsFrom, envsTo := pagination.SliceWindow(envsTotal, envsLimit, envsOffset)
	httpio.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"environments": envs[envsFrom:envsTo],
		"total":        envsTotal,
	})
}

// Create handles POST /api/v1/environments.
// Creates a new environment for the authenticated tenant.
func (h *EnvironmentHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	var req createEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInvalidRequest)
		return
	}

	// Validate name format (RFC 1123: lowercase alphanumeric + hyphens, no leading hyphen)
	if err := validation.ValidateEnvironmentName(req.Name); err != nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInvalidName.Wrap(err.Error()))
		return
	}

	// M2 v1.2 T4.1 + EC-8: reserved names are blocked tenant-wide,
	// regardless of project. Only the legacy production-bootstrap path
	// (no project, no API request) creates a `production` env. The user
	// cannot create (web, production) — surprise prevention.
	if reservedEnvironmentNames[req.Name] {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInvalidName.Wrap("environment name is reserved and cannot be used"))
		return
	}

	// M2 v1.2 T4.1 — `project` is REQUIRED.
	if req.Project == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("project is required: pass the project slug (not the project UUID) in the request body"))
		return
	}

	// B-059: declare the resource id for the audit row (was body-snooped
	// from the `project` key).
	auditctx.SetResourceID(ctx, req.Project)

	// M2 v1.2 T4.1 EC-1 — cross-tenant project gate (SEPA-CRITICAL).
	// Even when project field is set, it MUST belong to the caller's
	// tenant. Without this, a user of tenant A could pass a project
	// slug owned by tenant B and create an Environment that would land
	// in B's namespace. The lookup query MUST include `tenant_id = $1`.
	//
	// Nil projectStore → test fixtures and no-database boots, where there is
	// no projects table to check against; the gate is skipped there.
	if h.projectStore != nil {
		exists, perr := h.projectStore.ProjectExistsForTenant(ctx, uc.TenantID, req.Project)
		if perr != nil {
			slog.Error("failed to validate project",
				"tenant_id", uc.TenantID,
				"project", req.Project,
				"error", perr.Error(),
			)
			httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
			return
		}
		if !exists {
			// 404 (not 403) — matches the existence-leak-prevention
			// pattern already established for cross-tenant DELETE flows.
			httpio.WriteLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
				"PROJECT_NOT_FOUND",
				fmt.Sprintf("project %q not found in your account", req.Project)))
			return
		}
	}

	if err := h.envMgr.AddEnvironment(ctx, uc.TenantID, req.Project, req.Name); err != nil {
		// M2 v1.2 T4.1 EC-5 — duplicate (project, name) tuple MUST
		// surface as 409 CONFLICT, not 201 silent. The user needs to
		// know whether they actually created the env or it already
		// existed (concurrent dev clients, retry on 5xx flake, etc).
		if errors.Is(err, ports.ErrEnvironmentExists) {
			httpio.WriteLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
				"ENV_ALREADY_EXISTS",
				fmt.Sprintf("environment %q already exists in project %q", req.Name, req.Project)))
			return
		}
		slog.Error("failed to create environment",
			"tenant_id", uc.TenantID,
			"project", req.Project,
			"environment", req.Name,
			"error", err.Error(),
		)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	httpio.WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"name":    req.Name,
		"project": req.Project,
		"status":  "active",
	})
}

// Get handles GET /api/v1/environments/{name}.
// Returns detail for a single environment.
func (h *EnvironmentHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("environment name is required"))
		return
	}

	// M2 v1.2 T4.3 — `?project=` is optional. When set, the lookup
	// targets the (name, project) tuple. When empty, the legacy
	// project-agnostic lookup preserves backward compat (production
	// legacy entry resolves; first-match wins for tenant-wide queries).
	project := r.URL.Query().Get("project")
	detail, err := h.envMgr.GetEnvironmentDetail(ctx, uc.TenantID, project, name)
	if err != nil {
		// Issue #139 — an ambiguous project-less lookup is a CALLER problem,
		// not a server fault: the same env name exists in several projects and
		// none was named. Collapsing it into a 500 would tell the operator
		// nothing and page whoever owns the API. Answer 400 with the candidate
		// projects so the next command can succeed.
		var amb *ports.ErrAmbiguousEnvironment
		if errors.As(err, &amb) {
			httpio.WriteLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
				"ENVIRONMENT_AMBIGUOUS",
				fmt.Sprintf("environment '%s' exists in more than one project (%s); specify the project",
					name, strings.Join(amb.Projects, ", "))))
			return
		}
		slog.Error("failed to get environment detail",
			"tenant_id", uc.TenantID,
			"environment", name,
			"error", err.Error(),
		)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if detail == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.NewEnvironmentNotFound(name))
		return
	}

	httpio.WriteJSON(w, http.StatusOK, detail)
}

// Delete handles DELETE /api/v1/environments/{name}.
// Removes an environment. Refuses to delete "production".
func (h *EnvironmentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("environment name is required"))
		return
	}

	// Protect production environment from deletion
	if name == pkgdomain.DefaultPermanentEnv {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrForbidden.Wrap("the production environment cannot be deleted"))
		return
	}

	// M2 v1.2 T4.2 — `?project=` is REQUIRED for non-legacy deletes.
	// ADR D3 chose query-param shape over nested resource so the
	// existing route (DELETE /api/v1/environments/{name}) stays stable
	// while the tuple semantics become explicit at the contract layer.
	//
	// Legacy production lives at project="" (ADR D2): operators (or
	// service-auth callers cleaning up tenants) can still target it by
	// omitting `?project=`. The handler does NOT require the query
	// parameter for the protected `production` env path because the
	// existence-leak-prevention check (404 below) catches typos cleanly.
	project := r.URL.Query().Get("project")

	// M2 v1.2 T4.2 EC-7 — surface an orphan-App warning when the env
	// being removed still has an active release. The actual reaping is
	// async (operator side); the response payload tells the user what
	// to expect. Nil relLookup → no warning emitted (test / legacy). A failed
	// lookup cannot rule an active release out, so it warns too.
	var warnings []string
	if h.relLookup != nil {
		hasRel, err := h.relLookup.HasActiveRelease(ctx, uc.TenantID, project, name)
		if err != nil {
			slog.WarnContext(ctx, "active release lookup failed; warning about cleanup conservatively",
				"tenant_id", uc.TenantID, "project", project, "environment", name, "error", err)
		}
		if hasRel || err != nil {
			warnings = append(warnings, envDeleteCleanupWarning)
		}
	}

	if err := h.envMgr.RemoveEnvironment(ctx, uc.TenantID, project, name); err != nil {
		// AUDIT-12: map "not found" to 404 instead of silent success.
		if errors.Is(err, ports.ErrEnvironmentNotFound) {
			httpio.WriteLoopZeroError(w, loopzeroerrors.NewEnvironmentNotFound(name))
			return
		}
		slog.Error("failed to delete environment",
			"tenant_id", uc.TenantID,
			"environment", name,
			"error", err.Error(),
		)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	resp := map[string]interface{}{
		"deleted": name,
	}
	if project != "" {
		resp["project"] = project
	}
	if len(warnings) > 0 {
		resp["warnings"] = warnings
	}
	httpio.WriteJSON(w, http.StatusOK, resp)
}
