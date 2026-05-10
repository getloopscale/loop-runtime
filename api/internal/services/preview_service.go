// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/domain"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// PreviewBuildSubmitter is the minimal contract PreviewService needs from
// BuildOrchestrator for triggering a build for a preview environment.
//
// Defined narrowly so that:
//   1. PreviewService does not depend on the full BuildOrchestrator surface
//      (ISP).
//   2. Tests can supply a stub that asserts on the request without setting
//      up the full build pipeline.
type PreviewBuildSubmitter interface {
	SubmitBuild(ctx context.Context, req SubmitBuildRequest) (*SubmitBuildResponse, error)
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
	Store          db.PreviewStore
	Builds         PreviewBuildSubmitter
	CRCreator      PreviewCRCreator
	BaseDomain     string
	DefaultTTL     time.Duration
	Logger         *slog.Logger
	// Locker is optional — when set, CreatePreview wraps the Store/CR
	// write path in `pg_advisory_xact_lock(tenant, project, branch)` so a
	// rapid double-webhook for the same PR cannot race into two preview
	// rows. T1.2 of deep-review-2026-05-10-remediation-plan
	// (find_conc_preview_no_lock HIGH).
	Locker         EnvironmentLocker
}

// PreviewService implements domain.PreviewService and, additionally,
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

// CreatePreview implements domain.PreviewService.CreatePreview.
//
// Steps (per ADR D3 / D4):
//   1. Validate request fields.
//   2. UPSERT the DB row (db.PreviewStore.Create). This makes the preview
//      visible to API queries before any K8s/CR work.
//   3. Best-effort: ensure the PreviewEnvironment CR. The controller
//      reconciles namespace + workload from there.
//   4. If a build submitter is wired, trigger a build for the preview's
//      branch/commit. The build callback drives status transitions.
//
// Errors from steps 3 and 4 are logged but do NOT fail the create — the
// preview row is the source of truth for the API and the controller will
// retry on its own. This is the same resilience principle as the deploy
// flow (ARCH-002 fix in build_orchestrator).
func (s *PreviewService) CreatePreview(ctx context.Context, req domain.CreatePreviewRequest) (*domain.Preview, error) {
	if err := s.validate(req); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	ttl := s.deps.DefaultTTL
	if req.TTLHours > 0 {
		ttl = time.Duration(req.TTLHours) * time.Hour
	}
	expiresAt := now.Add(ttl)

	rec := &db.PreviewRecord{
		TenantID:  req.TenantID(),
		ProjectID: req.ProjectID,
		PRNumber:  req.PRNumber,
		Branch:    req.Branch,
		CommitSHA: req.CommitSHA,
		Status:    db.PreviewStatusCreating,
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
	url := s.previewURL(req.ProjectID, req.PRNumber)
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
		buildReq := SubmitBuildRequest{
			TenantID:    rec.TenantID,
			ProjectID:   rec.ProjectID,
			Environment: previewEnvName(rec.PRNumber),
			CommitSHA:   rec.CommitSHA,
			CreatedBy:   "preview-service",
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
		newStatus = db.PreviewStatusActive
	case "failed":
		newStatus = db.PreviewStatusFailed
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
func (s *PreviewService) validate(req domain.CreatePreviewRequest) error {
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
// Mirrors the shape used by the controller.
func (s *PreviewService) previewURL(projectID string, prNumber int) string {
	if s.deps.BaseDomain == "" {
		return ""
	}
	return fmt.Sprintf("https://pr%d-%s.preview.%s", prNumber, projectID, s.deps.BaseDomain)
}

// previewEnvName returns the synthetic environment label used when
// submitting a build for a preview. Distinguishes preview builds from
// normal staging/production builds.
func previewEnvName(prNumber int) string {
	return fmt.Sprintf("preview-pr-%d", prNumber)
}

// isPreviewNotFound centralizes the typed-error check so callers don't
// import errors directly.
func isPreviewNotFound(err error) bool {
	return err != nil && (err == loopzeroerrors.ErrPreviewNotFound || strings.Contains(err.Error(), "preview environment not found"))
}

// Compile-time assertion: PreviewService satisfies the route handler's
// PreviewService interface.
var _ domain.PreviewService = (*PreviewService)(nil)
