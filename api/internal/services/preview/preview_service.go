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
	"github.com/getloopscale/loopzero/api/internal/services/deploycontracts"

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
	// EnvTargets is the canonical env_targets store. When set,
	// OnBuildCompleted (status=succeeded) writes a kind='preview' row
	// for (tenant, project, preview-pr-N) so GitOps can render the
	// preview manifests symmetric with permanent envs (D2). Without
	// this, the env_target row is missing and the manifest renderer's
	// downstream consumers cannot resolve the preview env.
	//
	// Optional — when nil, the env_target write is skipped (legacy
	// fallback path used by tests).
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

	now := time.Now().UTC()
	ttl := s.deps.DefaultTTL
	if req.TTLHours > 0 {
		ttl = time.Duration(req.TTLHours) * time.Hour
	}
	expiresAt := now.Add(ttl)

	rec := &store.PreviewRecord{
		TenantID:  req.TenantID(),
		ProjectID: req.ProjectID,
		PRNumber:  req.PRNumber,
		Branch:    req.Branch,
		CommitSHA: req.CommitSHA,
		Status:    store.PreviewStatusCreating,
		ExpiresAt: expiresAt,
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
	url := s.previewURL(req.TenantID(), req.ProjectID, entryApp, req.PRNumber)
	if url != "" {
		u := url
		rec.URL = &u
	}

	// Step 2: persist. Create() runs UPSERT semantics for repeat PR pushes.
	//
	// T1.2 of deep-review-2026-05-10-remediation-plan: wrap the persist
	// in the environment lock when a Locker is wired. Lock key uses the
	// branch as the env equivalent — concurrent webhooks for different
	// PRs (different branches) of the same project run in parallel; the
	// same PR re-firing serializes. Falls back to non-locked path when
	// Locker is nil (test scope).
	persist := func(_ db.DBTX) error {
		return s.deps.Store.Create(ctx, nil, rec)
	}
	var persistErr error
	if s.deps.Locker != nil {
		persistErr = s.deps.Locker.WithLock(ctx, req.TenantID(), req.ProjectID, req.Branch, persist)
	} else {
		persistErr = persist(nil)
	}
	if persistErr != nil {
		return nil, fmt.Errorf("preview service: persist failed: %w", persistErr)
	}

	// D3 (plan 2026-06-07-preview-as-category-single-slot-plan.md):
	// substitute behavior. UPSERT preview_slot atomically with the new
	// PR — ON CONFLICT (tenant_id) DO UPDATE replaces the previous
	// active deployment. The slot is the single SOT for "this tenant's
	// active preview deployment" — the dashboard reads from it; the
	// dashboard's grid renders {permanent[], preview{}} where preview
	// = this row when ActivePRNumber != NULL.
	//
	// Best-effort: failures are logged but do NOT fail the create. The
	// preview_environments row already exists (step 2 succeeded); the
	// slot is a denormalized cache for the dashboard. A missing slot
	// row means the dashboard shows "No active branch" while the
	// underlying preview still works.
	if s.deps.SlotStore != nil {
		urlForSlot := ""
		if rec.URL != nil {
			urlForSlot = *rec.URL
		}
		if upsertErr := s.deps.SlotStore.Upsert(ctx, nil,
			rec.TenantID, rec.ProjectID,
			int32(rec.PRNumber), rec.Branch, "", urlForSlot,
		); upsertErr != nil {
			s.deps.Logger.Error("preview service: preview_slot upsert failed (dashboard slot will be stale)",
				"preview", rec.ID, "error", upsertErr)
		} else {
			s.deps.Logger.Info("preview service: preview_slot substituted (D3)",
				"preview", rec.ID, "pr", rec.PRNumber, "branch", rec.Branch)
		}
	}

	// Step 3: ensure CR. Best-effort.
	if s.deps.CRCreator != nil {
		spec := PreviewCRSpec{
			Name:      rec.ID,
			TenantID:  rec.TenantID,
			ProjectID: rec.ProjectID,
			PRNumber:  rec.PRNumber,
			Branch:    rec.Branch,
			ExpiresAt: expiresAt,
		}
		if err := s.deps.CRCreator.EnsurePreviewCR(ctx, spec); err != nil {
			s.deps.Logger.Error("preview service: ensure CR failed (will retry on reconcile)",
				"preview", rec.ID, "error", err)
		}
	}

	// Step 4: trigger build. Best-effort, but log loudly because a missing
	// build means the preview is stuck in 'creating'.
	if s.deps.Builds != nil {
		buildReq := deploycontracts.SubmitBuildRequest{
			TenantID:    rec.TenantID,
			ProjectID:   rec.ProjectID,
			Environment: previewEnvName(rec.PRNumber),
			CommitSHA:   rec.CommitSHA,
			CreatedBy:   "preview-service",
		}
		// LIVE catch 2026-06-07 Option B: prefer apps parsed from the PR
		// commit's loopzero.yaml (carries framework/path/build → drives
		// LanguageStrategy). Fall back to BuildAppsLookup (persisted apps
		// from latest SUCCEEDED build — no framework/path/build, fails
		// language-aware step resolution).
		if len(req.LoopZeroYamlApps) > 0 {
			buildReq.Apps = appsFromLoopZeroYaml(req.LoopZeroYamlApps)
			s.deps.Logger.Info("preview service: apps inherited from PR loopzero.yaml",
				"preview", rec.ID, "apps", len(buildReq.Apps))
		} else if s.deps.BuildApps != nil {
			apps, lookupErr := s.deps.BuildApps.LatestSucceededApps(ctx, rec.TenantID, rec.ProjectID)
			if lookupErr != nil {
				s.deps.Logger.Warn("preview service: app-spec lookup failed",
					"preview", rec.ID, "error", lookupErr)
			} else if len(apps) == 0 {
				s.deps.Logger.Warn("preview service: no prior SUCCEEDED build to copy apps from",
					"preview", rec.ID, "project", rec.ProjectID,
					"hint", "Deploy main branch at least once before opening a PR")
			} else {
				buildReq.Apps = apps
				s.deps.Logger.Info("preview service: apps inherited from latest SUCCEEDED build (no loopzero.yaml in PR)",
					"preview", rec.ID, "apps", len(apps))
			}
		}
		resp, err := s.deps.Builds.SubmitBuild(ctx, buildReq)
		if err != nil {
			s.deps.Logger.Error("preview service: build submission failed",
				"preview", rec.ID, "error", err)
		} else if resp != nil {
			if updErr := s.deps.Store.UpdateBuild(ctx, nil, rec.ID, resp.BuildID, rec.CommitSHA); updErr != nil {
				s.deps.Logger.Error("preview service: failed to link build to preview",
					"preview", rec.ID, "build", resp.BuildID, "error", updErr)
			}
		}
	}

	return rec.ToDomain(), nil
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

// OnBuildCompleted is invoked by the build callback path when a build
// belonging to a preview transitions to SUCCEEDED or FAILED.
//
// status: domain build status from the callback ("succeeded" or "failed").
func (s *PreviewService) OnBuildCompleted(ctx context.Context, previewID, buildID, status string) error {
	var newStatus string
	switch strings.ToLower(status) {
	case "succeeded":
		newStatus = store.PreviewStatusActive
	case "failed":
		newStatus = store.PreviewStatusFailed
	default:
		return fmt.Errorf("preview service: unsupported build callback status %q", status)
	}
	if err := s.deps.Store.UpdateStatus(ctx, nil, previewID, newStatus); err != nil {
		return fmt.Errorf("preview service: update status failed: %w", err)
	}
	s.deps.Logger.Info("preview status updated from build callback",
		"preview", previewID, "build", buildID, "status", newStatus)
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

// isPreviewNotFound centralizes the typed-error check so callers don't
// import errors directly.
func isPreviewNotFound(err error) bool {
	return err != nil && (errors.Is(err, loopzeroerrors.ErrPreviewNotFound) || strings.Contains(err.Error(), "preview environment not found"))
}

// Compile-time assertion: PreviewService satisfies the route handler's
// PreviewService interface.
var _ ports.PreviewService = (*PreviewService)(nil)

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
