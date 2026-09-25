// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package preview is the preview-environment bounded context. M9 Step 6.
package preview

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/ports"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
	"github.com/getloopscale/loopzero/pkg/naming"
)

// PreviewBuildSubmitter is the minimal contract PreviewService needs from
// BuildOrchestrator for triggering a build for a preview environment.
//
// Defined narrowly so that:
//  1. PreviewService does not depend on the full BuildOrchestrator surface
//     (ISP).
//  2. Tests can supply a stub that asserts on the request without setting
//     up the full build pipeline.
type PreviewBuildSubmitter interface {
	SubmitBuild(ctx context.Context, req deploycontracts.SubmitBuildRequest) (*deploycontracts.SubmitBuildResponse, error)
}

// PreviewBuildAppsLookup resolves the AppSpec list to use when submitting
// a preview build. Pattern: copy from the most recent SUCCEEDED build of
// the same (tenant, project) — the preview is the same project at a
// different commit, so the app structure (names/ports/types) is stable.
//
// LIVE catch 2026-06-07: without this, preview build submissions hit
// `WorkflowSpec.Plan must be non-nil with at least one app` because the
// PR webhook handler doesn't parse loopzero.yaml from the PR branch source
// (only push webhooks do, via WebhookBuildRequest.LoopZeroYaml).
type PreviewBuildAppsLookup interface {
	LatestSucceededApps(ctx context.Context, tenantID, projectID string) ([]deploycontracts.AppSpec, error)
}

// PreviewCRCreator is the optional contract for creating PreviewEnvironment
// custom resources on the management cluster. When a PreviewService is
// constructed without one, CR provisioning is skipped and the operator is
// expected to be driven externally (or tests run without K8s).
type PreviewCRCreator interface {
	EnsurePreviewCR(ctx context.Context, spec PreviewCRSpec) error
	DeletePreviewCR(ctx context.Context, tenantID, projectID string, prNumber int) error
}

// PreviewCRSpec mirrors the fields the controller reads from
// PreviewEnvironment.Spec.
type PreviewCRSpec struct {
	Name      string
	TenantID  string
	ProjectID string
	PRNumber  int
	Branch    string
	ReleaseID string
	ExpiresAt time.Time
	// PublicURL is the preview's user-facing URL (pkg/naming.PreviewURL).
	PublicURL string
}

// PreviewServiceDeps bundles dependencies for PreviewService.
//
// All fields except Store are optional; missing dependencies degrade
// gracefully:
//   - Builds: no build is triggered (preview row is created in 'creating'
//     status; an external process is expected to drive it forward).
//   - CRCreator: no CR is created (the operator must already be reconciling
//     external CRs).
//   - Logger: defaults to slog.Default().
type PreviewServiceDeps struct {
	Store  store.PreviewStore
	Builds PreviewBuildSubmitter
	// BuildApps is optional. When set, the preview service uses it to
	// populate SubmitBuildRequest.Apps from the most recent SUCCEEDED
	// build of the project. LIVE catch 2026-06-07 — without this the
	// build orchestrator rejects with "WorkflowSpec.Plan must be non-nil".
	BuildApps  PreviewBuildAppsLookup
	CRCreator  PreviewCRCreator
	BaseDomain string
	// PreviewPublicDomain is the wildcard root (Caddy/CF) for public
	// preview URLs. Empty falls back to BaseDomain — see config.go
	// PreviewPublicDomain doc for the canonical URL convention.
	PreviewPublicDomain string
	DefaultTTL          time.Duration
	Logger              *slog.Logger
	// Locker is optional — when set, CreatePreview wraps the Store/CR
	// write path in `pg_advisory_xact_lock(tenant, project, branch)` so a
	// rapid double-webhook for the same PR cannot race into two preview
	// rows. T1.2 of deep-review-2026-05-10-remediation-plan
	// (find_conc_preview_no_lock HIGH).
	Locker deploycontracts.EnvironmentLocker

	// SlotStore is the single-slot SOT for preview deployments per tenant
	// (D2/D3 of plan 2026-06-07-preview-as-category-single-slot-plan.md).
	// When set, CreatePreview atomically substitutes the active PR via
	// UpsertPreviewSlot (ON CONFLICT DO UPDATE on tenant_id) and
	// DeletePreviewByPR clears the slot.
	//
	// Optional — when nil, preview lifecycle still works via PreviewStore
	// + CR; the single-slot SOT is skipped (legacy fallback, used in
	// tests that don't exercise the substitute behavior).
	SlotStore store.PreviewSlotStore
	// EnvTargets is the canonical env_targets store. The kind='preview' row
	// for (tenant, project, preview-pr-N) is written at activation by the
	// container delivery activator (SetPreviewDesired); this service only
	// removes it when the PR closes (DeletePreviewByPR).
	//
	// Optional — when nil, that cleanup is skipped (tests).
	EnvTargets store.EnvironmentTargetStore
}

// PreviewService implements ports.PreviewService and, additionally,
// provides webhook-driven hooks for the GitHub PR flow (T3.1).
type PreviewService struct {
	deps PreviewServiceDeps
}

// NewPreviewService constructs a PreviewService. Returns an error when
// required dependencies are missing.
func NewPreviewService(deps PreviewServiceDeps) (*PreviewService, error) {
	if deps.Store == nil {
		return nil, fmt.Errorf("preview service: Store is required")
	}
	if deps.DefaultTTL <= 0 {
		deps.DefaultTTL = 72 * time.Hour
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &PreviewService{deps: deps}, nil
}

// CreatePreview implements ports.PreviewService.CreatePreview.
//
// Steps (per ADR D3 / D4):
//  1. Validate request fields.
//  2. UPSERT the DB row (store.PreviewStore.Create). This makes the preview
//     visible to API queries before any K8s/CR work.
//  3. Best-effort: ensure the PreviewEnvironment CR. The controller
//     reconciles namespace + workload from there.
//  4. If a build submitter is wired, trigger a build for the preview's
//     branch/commit. The build callback drives status transitions.
//
// Errors from steps 3 and 4 are logged but do NOT fail the create — the
// preview row is the source of truth for the API and the controller will
// retry on its own. This is the same resilience principle as the deploy
// flow (ARCH-002 fix in build_orchestrator).
func (s *PreviewService) CreatePreview(ctx context.Context, req ports.CreatePreviewRequest) (*ports.Preview, error) {
	if err := s.validate(req); err != nil {
		return nil, err
	}
	rec := s.newPreviewRecord(req)
	if err := s.persistPreview(ctx, rec); err != nil {
		return nil, fmt.Errorf("preview service: persist failed: %w", err)
	}
	s.upsertSlot(ctx, rec)
	s.ensureCR(ctx, rec)
	s.submitPreviewBuild(ctx, rec, req.LoopZeroYamlApps)
	return rec.ToDomain(), nil
}

// newPreviewRecord builds the row to persist: TTL, optional GitHub metadata
// and the public URL.
func (s *PreviewService) newPreviewRecord(req ports.CreatePreviewRequest) *store.PreviewRecord {
	ttl := s.deps.DefaultTTL
	if req.TTLHours > 0 {
		ttl = time.Duration(req.TTLHours) * time.Hour
	}
	rec := &store.PreviewRecord{
		TenantID:  req.TenantID(),
		ProjectID: req.ProjectID,
		PRNumber:  req.PRNumber,
		Branch:    req.Branch,
		CommitSHA: req.CommitSHA,
		Status:    store.PreviewStatusCreating,
		ExpiresAt: time.Now().UTC().Add(ttl),
	}
	if req.RepoFullName != "" {
		rfn := req.RepoFullName
		rec.RepoFullName = &rfn
	}
	if req.InstallationID != 0 {
		iid := req.InstallationID
		rec.InstallationID = &iid
	}
	// Entry app is picked from loopzero.yaml's apps map (first server/frontend
	// in sorted key order). Without it, the URL omits the app segment —
	// it surfaces a structurally valid public hostname but won't match an
	// IngressRoute (proxy 404). Best-effort; the dashboard repolls if the
	// app set updates after CR creation.
	entryApp := entryAppFromLoopZeroYaml(req.LoopZeroYamlApps)
	if url := s.previewURL(req.TenantID(), req.ProjectID, entryApp, req.PRNumber); url != "" {
		rec.URL = &url
	}
	return rec
}

// persistPreview writes the row (UPSERT semantics for repeat PR pushes).
//
// T1.2 of deep-review-2026-05-10-remediation-plan: when a Locker is wired
// the write runs inside the environment lock, on the lock's transaction, and
// the lock is keyed on the preview environment (previewEnvName) — the same
// key the preview's build locks on. Different PRs of one project run in
// parallel; the same PR re-firing serializes with itself and its build.
// Without a Locker (test scope) the write goes through the pool.
func (s *PreviewService) persistPreview(ctx context.Context, rec *store.PreviewRecord) error {
	persist := func(tx db.DBTX) error {
		return s.deps.Store.Create(ctx, tx, rec)
	}
	if s.deps.Locker == nil {
		return persist(nil)
	}
	return s.deps.Locker.WithLock(ctx, rec.TenantID, rec.ProjectID, previewEnvName(rec.PRNumber), persist)
}

// upsertSlot substitutes the tenant's single preview slot with this PR (D3,
// plan 2026-06-07-preview-as-category-single-slot-plan.md). ON CONFLICT
// (tenant_id) DO UPDATE replaces the previous active deployment; the slot is
// the dashboard's source for "this tenant's active preview".
//
// Best-effort: the preview row already exists and the slot is a
// denormalized cache, so a failure is logged and the create proceeds — the
// dashboard shows "No active branch" while the preview still works.
func (s *PreviewService) upsertSlot(ctx context.Context, rec *store.PreviewRecord) {
	if s.deps.SlotStore == nil {
		return
	}
	urlForSlot := ""
	if rec.URL != nil {
		urlForSlot = *rec.URL
	}
	if err := s.deps.SlotStore.Upsert(ctx, nil,
		rec.TenantID, rec.ProjectID,
		int32(rec.PRNumber), rec.Branch, "", urlForSlot,
	); err != nil {
		s.deps.Logger.Error("preview service: preview_slot upsert failed (dashboard slot will be stale)",
			"preview", rec.ID, "error", err)
		return
	}
	s.deps.Logger.Info("preview service: preview_slot substituted (D3)",
		"preview", rec.ID, "pr", rec.PRNumber, "branch", rec.Branch)
}

// ensureCR asks the controller side to create the PreviewEnvironment CR.
// Best-effort: the controller retries on reconcile.
func (s *PreviewService) ensureCR(ctx context.Context, rec *store.PreviewRecord) {
	if s.deps.CRCreator == nil {
		return
	}
	spec := PreviewCRSpec{
		Name:      rec.ID,
		TenantID:  rec.TenantID,
		ProjectID: rec.ProjectID,
		PRNumber:  rec.PRNumber,
		Branch:    rec.Branch,
		ExpiresAt: rec.ExpiresAt,
	}
	if rec.URL != nil {
		spec.PublicURL = *rec.URL
	}
	if err := s.deps.CRCreator.EnsurePreviewCR(ctx, spec); err != nil {
		s.deps.Logger.Error("preview service: ensure CR failed (will retry on reconcile)",
			"preview", rec.ID, "error", err)
	}
}

// submitPreviewBuild triggers the preview's build and links it to the row.
// Best-effort, but logged loudly: a missing build leaves the preview stuck in
// 'creating'.
func (s *PreviewService) submitPreviewBuild(ctx context.Context, rec *store.PreviewRecord, loopzeroYamlApps map[string]map[string]interface{}) {
	if s.deps.Builds == nil {
		return
	}
	resp, err := s.deps.Builds.SubmitBuild(ctx, deploycontracts.SubmitBuildRequest{
		TenantID:    rec.TenantID,
		ProjectID:   rec.ProjectID,
		Environment: previewEnvName(rec.PRNumber),
		CommitSHA:   rec.CommitSHA,
		CreatedBy:   "preview-service",
		Apps:        s.resolvePreviewApps(ctx, rec, loopzeroYamlApps),
	})
	if err != nil {
		s.deps.Logger.Error("preview service: build submission failed",
			"preview", rec.ID, "error", err)
		return
	}
	if resp == nil {
		return
	}
	if err := s.deps.Store.UpdateBuild(ctx, nil, rec.ID, resp.BuildID, rec.CommitSHA); err != nil {
		s.deps.Logger.Error("preview service: failed to link build to preview",
			"preview", rec.ID, "build", resp.BuildID, "error", err)
	}
}

// resolvePreviewApps picks the app specs the preview builds with.
//
// LIVE catch 2026-06-07 Option B: apps parsed from the PR commit's loopzero.yaml
// are preferred (they carry framework/path/build, which drive
// LanguageStrategy). Otherwise the apps of the latest SUCCEEDED build are
// reused; those lack framework/path/build. With neither, the build is
// submitted without apps and the lookup outcome is logged.
func (s *PreviewService) resolvePreviewApps(ctx context.Context, rec *store.PreviewRecord, loopzeroYamlApps map[string]map[string]interface{}) []deploycontracts.AppSpec {
	if len(loopzeroYamlApps) > 0 {
		apps := appsFromLoopZeroYaml(loopzeroYamlApps)
		s.deps.Logger.Info("preview service: apps inherited from PR loopzero.yaml",
			"preview", rec.ID, "apps", len(apps))
		return apps
	}
	if s.deps.BuildApps == nil {
		return nil
	}
	apps, err := s.deps.BuildApps.LatestSucceededApps(ctx, rec.TenantID, rec.ProjectID)
	switch {
	case err != nil:
		s.deps.Logger.Warn("preview service: app-spec lookup failed",
			"preview", rec.ID, "error", err)
		return nil
	case len(apps) == 0:
		s.deps.Logger.Warn("preview service: no prior SUCCEEDED build to copy apps from",
			"preview", rec.ID, "project", rec.ProjectID,
			"hint", "Deploy main branch at least once before opening a PR")
		return nil
	default:
		s.deps.Logger.Info("preview service: apps inherited from latest SUCCEEDED build (no loopzero.yaml in PR)",
			"preview", rec.ID, "apps", len(apps))
		return apps
	}
}

// DeletePreviewByPR is the entry point for the GitHub `pull_request: closed`
// webhook. It marks the preview row for deletion and asks the CRCreator to
// delete the CR (the controller drives namespace teardown via the
// finalizer).
func (s *PreviewService) DeletePreviewByPR(ctx context.Context, tenantID, projectID string, prNumber int) error {
	preview, err := s.deps.Store.GetByPR(ctx, tenantID, projectID, prNumber)
	if err != nil {
		// PreviewNotFound is acceptable — closing a PR that never had a
		// preview is a no-op, not an error.
		if isPreviewNotFound(err) {
			return nil
		}
		return fmt.Errorf("preview service: lookup by PR failed: %w", err)
	}

	if err := s.deps.Store.RequestDeletion(ctx, tenantID, preview.ID); err != nil {
		return fmt.Errorf("preview service: request deletion failed: %w", err)
	}

	// D6 cleanup chain (plan 2026-06-07-preview-as-category-single-slot-
	// plan.md): clear the single-slot SOT + delete the env_target
	// preview row. Best-effort — failures are logged but do NOT fail
	// the PR-close path. The preview_environments row was already
	// marked for deletion above (step 1 of cleanup); the slot + env_target
	// rows are secondary (dashboard + manifest renderer reads). Stale
	// rows here cause cosmetic drift, not correctness bugs.
	envName := previewEnvName(prNumber)
	if s.deps.SlotStore != nil {
		// 2026-06-07 per-project (migration 000028): clear only the
		// slot of THIS project. Other projects of the same tenant
		// keep their slots intact.
		if err := s.deps.SlotStore.Clear(ctx, nil, tenantID, projectID); err != nil {
			s.deps.Logger.Error("preview service: preview_slot clear failed (dashboard slot will be stale)",
				"preview", preview.ID, "error", err)
		}
	}
	if s.deps.EnvTargets != nil {
		if err := s.deps.EnvTargets.DeleteByKind(ctx, nil, tenantID, projectID, envName, store.EnvironmentTargetKindPreview); err != nil {
			s.deps.Logger.Error("preview service: env_target preview delete failed",
				"preview", preview.ID, "env", envName, "error", err)
		}
	}

	if s.deps.CRCreator != nil {
		if err := s.deps.CRCreator.DeletePreviewCR(ctx, tenantID, projectID, prNumber); err != nil {
			s.deps.Logger.Error("preview service: delete CR failed (controller cleanup may be delayed)",
				"preview", preview.ID, "error", err)
		}
	}
	return nil
}

// validate enforces the same invariants the HTTP handler enforces.
// Re-checked here so non-HTTP entry points (webhook, internal callers) are
// also safe.
func (s *PreviewService) validate(req ports.CreatePreviewRequest) error {
	if strings.TrimSpace(req.TenantID()) == "" {
		return loopzeroerrors.ErrMissingField.Wrap("tenant_id is required")
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		return loopzeroerrors.ErrMissingField.Wrap("project_id is required")
	}
	if req.PRNumber <= 0 {
		return loopzeroerrors.ErrMissingField.Wrap("pr_number must be positive")
	}
	if strings.TrimSpace(req.Branch) == "" {
		return loopzeroerrors.ErrMissingField.Wrap("branch is required")
	}
	if strings.TrimSpace(req.CommitSHA) == "" {
		return loopzeroerrors.ErrMissingField.Wrap("commit_sha is required")
	}
	return nil
}

// previewURL composes the public URL exposed to users for a preview.
//
// J-23 wildcard convention (sponsor verbatim 2026-06-07 "Continuar com
// wildcard CF + Caddy wildcard block agora"): the URL mirrors the
// IngressRoute Host emitted by appHostname() so the public wildcard
// `*.<PreviewPublicDomain>` proxied by Caddy reverse-proxies cleanly to
// Traefik on the workload cluster.
//
// Shape: https://<tenant>-<project>-<entryApp>-preview-pr-N.<publicDomain>
//
// When entryApp is unknown (no loopzero.yaml apps available, legacy callers)
// the URL omits the app segment — the URL won't resolve to a Traefik
// IngressRoute (404 at the proxy) but the architecture pattern is correct
// and the dashboard surfaces a structurally valid URL.
//
// Public domain resolution: prefer PreviewPublicDomain (the Caddy/CF
// wildcard root, e.g., `preview-dev.loopzero.example`); fall back to
// BaseDomain when empty (legacy local behavior where Caddy is absent and
// the internal SNI domain == public domain).
// previewURL delegates to pkg/naming.PreviewURL — T2.5 of plan
// 2026-06-11-code-review-remediation extracted the formula so the
// canonical preview URL composition lives at one source of truth
// next to pkg/naming.Namespace + IsPreviewEnv + PreviewEnvName.
//
// Public-domain resolution stays here because it is a service-wiring
// concern (PreviewPublicDomain vs legacy BaseDomain), not a naming
// concern.
func (s *PreviewService) previewURL(tenantID, projectID, entryApp string, prNumber int) string {
	publicDomain := s.deps.PreviewPublicDomain
	if publicDomain == "" {
		publicDomain = s.deps.BaseDomain
	}
	return naming.PreviewURL(tenantID, projectID, entryApp, publicDomain, prNumber)
}

// entryAppFromLoopZeroYaml picks the first server-class app from loopzero.yaml's
// apps map — defined as the app that gets an IngressRoute (server or
// frontend type, or unspecified type which defaults to server). Worker
// apps are skipped because they receive no traffic. Returns "" when no
// suitable app is found; callers fall back to the appless URL form.
//
// Ordering: iterates apps map in sorted key order so the choice is
// deterministic across reconciles. Returns the first match.
func entryAppFromLoopZeroYaml(apps map[string]map[string]interface{}) string {
	if len(apps) == 0 {
		return ""
	}
	keys := make([]string, 0, len(apps))
	for k := range apps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, name := range keys {
		spec := apps[name]
		appType, _ := spec["type"].(string)
		switch appType {
		case "", "server":
			return name
		}
	}
	return ""
}

// previewEnvName delegates to pkg/naming.PreviewEnvName (T2.5).
// The package-local wrapper is kept to minimize call-site churn.
func previewEnvName(prNumber int) string {
	return naming.PreviewEnvName(prNumber)
}

// isPreviewNotFound matches only the typed store sentinel. A message that
// merely reads like it is a different failure and must propagate.
func isPreviewNotFound(err error) bool {
	return errors.Is(err, loopzeroerrors.ErrPreviewNotFound)
}

// Compile-time assertion: PreviewService satisfies the route handler's
// PreviewService interface.
var _ PreviewCreator = (*PreviewService)(nil)

// appsFromLoopZeroYaml — deprecated thin wrapper around the canonical
// ConvertLoopZeroYamlApps. Kept for in-package call sites that still use
// the short name; the implementation now delegates to the single source
// of truth so push-flow and PR-flow can never drift again (sponsor
// LIVE-caught 2026-06-18: this function had silently fallen out of sync
// with autodeploy_adapters.go — losing the `start` field — breaking
// preview builds with "no declared start command").
func appsFromLoopZeroYaml(rawApps map[string]map[string]interface{}) []deploycontracts.AppSpec {
	return deploycontracts.ConvertLoopZeroYamlApps(rawApps)
}
