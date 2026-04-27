// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package github

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/domain"
	"github.com/loopzero/loopzero/api/internal/github/webhook"
	"github.com/loopzero/loopzero/api/internal/routes"
)

// PreviewLifecycleService is the slice of services.PreviewService that the
// PR webhook handler needs. Defined narrowly per ISP so the deployer can
// be tested without standing up the full service.
type PreviewLifecycleService interface {
	CreatePreview(ctx context.Context, req routes.CreatePreviewRequest) (*domain.Preview, error)
	DeletePreviewByPR(ctx context.Context, tenantID, projectID string, prNumber int) error
}

// RepoLookup looks up a project repository by full GitHub name. Subset of
// db.PostgresRepositoryStore — kept narrow for testability.
type RepoLookup interface {
	GetByRepo(ctx context.Context, repoFullName string) (*db.ProjectRepository, error)
}

// PreviewDeployer handles GitHub `pull_request` webhook events.
//
// Mapping (per plan §T3.1):
//
//   action      | service call
//   ------------+---------------------------------------------------------
//   opened      | CreatePreview
//   synchronize | CreatePreview (UPSERT — same row, new commit)
//   reopened    | CreatePreview
//   closed      | DeletePreviewByPR (regardless of merged=true/false)
//
// Other actions (labeled, assigned, edited, ...) are ignored.
//
// Rule: only repos where `auto_deploy = true` are processed. This is the
// same gate auto-deploy on push uses, ensuring connect → disconnect
// transitions are observed by both flows uniformly.
type PreviewDeployer struct {
	previewService PreviewLifecycleService
	repoStore      RepoLookup
	logger         *slog.Logger
}

// NewPreviewDeployer constructs a PreviewDeployer.
func NewPreviewDeployer(svc PreviewLifecycleService, repoStore RepoLookup, logger *slog.Logger) *PreviewDeployer {
	if logger == nil {
		logger = slog.Default()
	}
	return &PreviewDeployer{
		previewService: svc,
		repoStore:      repoStore,
		logger:         logger,
	}
}

// HandlePR is registered as the webhook handler's OnPullRequest callback.
// Returns nil for ignored events (unknown action, repo not connected,
// auto-deploy disabled, missing PR data) — those are not errors. Errors
// here are logged by the webhook dispatcher; nothing is returned to
// GitHub.
func (d *PreviewDeployer) HandlePR(event webhook.PullRequestEvent) error {
	ctx := context.Background()

	log := d.logger.With(
		"action", event.Action,
		"repo", event.Repository.FullName,
		"pr", event.Number,
	)

	// Step 1: dispatch only known actions before any DB lookup.
	switch event.Action {
	case "opened", "synchronize", "reopened", "closed":
		// proceed
	default:
		log.Debug("preview deploy: ignored PR action")
		return nil
	}

	// Step 2: lookup repo → (tenant, project).
	if d.repoStore == nil {
		log.Debug("preview deploy: no repo store configured, skipping")
		return nil
	}
	repo, err := d.repoStore.GetByRepo(ctx, event.Repository.FullName)
	if err != nil {
		// Lookup failure isn't a hard fail — log and skip. The PR may be
		// for a repo we don't manage; GitHub still gets 200 from us.
		log.Error("preview deploy: repo lookup failed", "error", err)
		return nil
	}
	if repo == nil {
		log.Debug("preview deploy: repo not connected, ignoring")
		return nil
	}

	// Step 3: enforce auto-deploy guard.
	if !repo.AutoDeploy {
		log.Debug("preview deploy: auto_deploy disabled, skipping",
			"tenant", repo.TenantID, "project", repo.ProjectName)
		return nil
	}

	// Step 4: dispatch.
	if d.previewService == nil {
		log.Warn("preview deploy: no preview service configured, skipping")
		return nil
	}

	switch event.Action {
	case "closed":
		// Tear down regardless of merged state. The PR is gone — the
		// preview should be too.
		return d.handleClosed(ctx, log, repo, event)
	default: // opened/synchronize/reopened
		return d.handleCreate(ctx, log, repo, event)
	}
}

// handleCreate builds a CreatePreviewRequest from the PR event and forwards
// it to the preview service. Validates the minimum data the service
// requires; missing data is logged but not surfaced as an error.
func (d *PreviewDeployer) handleCreate(
	ctx context.Context,
	log *slog.Logger,
	repo *db.ProjectRepository,
	event webhook.PullRequestEvent,
) error {
	branch := event.PullRequest.Head.Ref
	commit := event.PullRequest.Head.SHA
	if branch == "" || commit == "" {
		log.Warn("preview deploy: missing branch or commit on PR event, skipping",
			"branch_present", branch != "", "commit_present", commit != "")
		return nil
	}
	if event.Number <= 0 {
		log.Warn("preview deploy: invalid PR number on event, skipping", "number", event.Number)
		return nil
	}

	req := routes.CreatePreviewRequest{
		ProjectID:      repo.ProjectName,
		PRNumber:       event.Number,
		Branch:         branch,
		CommitSHA:      commit,
		RepoFullName:   event.Repository.FullName,
		InstallationID: event.Installation.ID,
	}
	req.SetTenantID(repo.TenantID)

	preview, err := d.previewService.CreatePreview(ctx, req)
	if err != nil {
		log.Error("preview deploy: CreatePreview failed", "error", err)
		return fmt.Errorf("preview deploy create: %w", err)
	}
	log.Info("preview deploy: preview triggered",
		"preview_id", previewID(preview),
		"tenant", repo.TenantID,
		"project", repo.ProjectName,
		"branch", branch,
		"commit", short(commit),
	)
	return nil
}

// handleClosed forwards the close event to the preview service. Idempotent:
// closing a PR that never had a preview is a no-op.
func (d *PreviewDeployer) handleClosed(
	ctx context.Context,
	log *slog.Logger,
	repo *db.ProjectRepository,
	event webhook.PullRequestEvent,
) error {
	if event.Number <= 0 {
		log.Warn("preview deploy: closed event with invalid PR number, skipping",
			"number", event.Number)
		return nil
	}
	if err := d.previewService.DeletePreviewByPR(ctx, repo.TenantID, repo.ProjectName, event.Number); err != nil {
		log.Error("preview deploy: DeletePreviewByPR failed", "error", err)
		return fmt.Errorf("preview deploy close: %w", err)
	}
	log.Info("preview deploy: preview teardown requested",
		"tenant", repo.TenantID,
		"project", repo.ProjectName,
		"merged", event.PullRequest.Merged,
	)
	return nil
}

// previewID returns the preview ID for logging; safe on nil.
func previewID(p *domain.Preview) string {
	if p == nil {
		return ""
	}
	return p.ID
}

// short truncates a commit SHA for log readability.
func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
