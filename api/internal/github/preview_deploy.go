// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package github

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/getloopscale/loop-runtime/api/internal/github/webhook"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// PreviewLifecycleService is the slice of preview.PreviewService that the
// PR webhook handler needs. Defined narrowly per ISP so the deployer can
// be tested without standing up the full service.
type PreviewLifecycleService interface {
	CreatePreview(ctx context.Context, req ports.CreatePreviewRequest) (*ports.Preview, error)
	DeletePreviewByPR(ctx context.Context, tenantID, projectID string, prNumber int) error
}

// ConnectedRepo is what the preview deployer needs to know about a GitHub
// repository linked to a project. It is this package's own type, so the
// persistence model can change without reaching the GitHub integration.
type ConnectedRepo struct {
	TenantID    string
	ProjectName string
	AutoDeploy  bool
}

// RepoLookup finds the project a GitHub repository is linked to. It returns
// (nil, nil) for a repository that is not linked; an error is a real failure.
type RepoLookup interface {
	GetByRepo(ctx context.Context, repoFullName string) (*ConnectedRepo, error)
}

// SourceBundlePreparer prepares the source bundle on S3 for a commit
// before the preview build runs AND returns the parsed loopzero.yaml so
// the preview deployer can pass apps with framework/path/build to the
// build orchestrator (resolves LanguageStrategy to language-aware
// step names; LIVE catch 2026-06-07 J-23 bug #3).
//
// nil LoopZeroYamlMinimal returned = no loopzero.yaml in repo (preview build
// will fail noisily — log + warn upstream).
// Subset of AutoDeployer; kept narrow for testability.
type SourceBundlePreparer interface {
	PrepareSourceBundle(ctx context.Context, repoFullName, commitSHA string, installationID int64, tenantID string) (*LoopZeroYamlMinimal, error)
}

// PreviewDeployer handles GitHub `pull_request` webhook events.
//
// Mapping (per plan §T3.1):
//
//	action      | service call
//	------------+---------------------------------------------------------
//	opened      | CreatePreview
//	synchronize | CreatePreview (UPSERT — same row, new commit)
//	reopened    | CreatePreview
//	closed      | DeletePreviewByPR (regardless of merged=true/false)
//
// Other actions (labeled, assigned, edited, ...) are ignored.
//
// Rule: only repos where `auto_deploy = true` are processed. This is the
// same gate auto-deploy on push uses, ensuring connect → disconnect
// transitions are observed by both flows uniformly.
type PreviewDeployer struct {
	previewService PreviewLifecycleService
	repoStore      RepoLookup
	bundlePrep     SourceBundlePreparer // optional but required for real builds
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

// WithBundlePreparer wires the SourceBundlePreparer dep so PR webhooks
// upload the source bundle to S3 before the preview build runs.
func (d *PreviewDeployer) WithBundlePreparer(p SourceBundlePreparer) *PreviewDeployer {
	d.bundlePrep = p
	return d
}

// HandlePR is registered as the webhook handler's OnPullRequest callback.
// Returns nil for ignored events (unknown action, repo not connected,
// auto-deploy disabled, missing PR data) — those are not errors. Errors
// here are logged by the webhook dispatcher; nothing is returned to
// GitHub.
func (d *PreviewDeployer) HandlePR(ctx context.Context, event webhook.PullRequestEvent) error {

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
		// An unmanaged repo comes back as (nil, nil) below; an error here is a
		// real failure, and swallowing it would lose the preview silently.
		return fmt.Errorf("preview deploy: look up repo %s: %w", event.Repository.FullName, err)
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
	repo *ConnectedRepo,
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

	req := ports.CreatePreviewRequest{
		ProjectID:      repo.ProjectName,
		PRNumber:       event.Number,
		Branch:         branch,
		CommitSHA:      commit,
		RepoFullName:   event.Repository.FullName,
		InstallationID: event.Installation.ID,
	}
	req.SetTenantID(repo.TenantID)

	// LIVE catch 2026-06-07 J-23: pre-flight the source bundle upload to
	// S3 BEFORE asking the preview service to submit the build. Without
	// this, the build clone-repo step fails with "Object does not exist".
	// ALSO returns the parsed loopzero.yaml so we can hand framework/path/build
	// to the orchestrator (drives LanguageStrategy step naming).
	if d.bundlePrep != nil {
		loopzeroYaml, prepErr := d.bundlePrep.PrepareSourceBundle(ctx,
			event.Repository.FullName, commit, event.Installation.ID, repo.TenantID,
		)
		if prepErr != nil {
			log.Error("preview deploy: source bundle prep failed", "error", prepErr)
			return fmt.Errorf("preview deploy bundle: %w", prepErr)
		}
		log.Info("preview deploy: source bundle uploaded", "commit", short(commit))
		if loopzeroYaml != nil {
			req.LoopZeroYamlApps = loopzeroYaml.Apps
			log.Info("preview deploy: loopzero.yaml apps inherited", "apps", len(loopzeroYaml.Apps))
		}
	} else {
		log.Warn("preview deploy: no bundle preparer wired; build will likely fail at clone-repo")
	}

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
	repo *ConnectedRepo,
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
func previewID(p *ports.Preview) string {
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
