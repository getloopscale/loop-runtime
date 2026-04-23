// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/loopzero/loopzero/api/internal/db"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
	"github.com/loopzero/loopzero/pkg/id"
)

// PromotionReleaseStore abstracts release persistence for the promotion orchestrator (DIP).
type PromotionReleaseStore interface {
	GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*db.Release, error)
	Create(ctx context.Context, tx db.DBTX, release *db.Release) error
	UpdateStatus(ctx context.Context, tx db.DBTX, id string, event string) (*db.Release, error)
}

// PromotionEnvTargetStore abstracts environment target operations for promotion (DIP).
type PromotionEnvTargetStore interface {
	Get(ctx context.Context, tenantID, projectID, env string) (*db.EnvironmentTarget, error)
	SetDesired(ctx context.Context, tx db.DBTX, tenantID, projectID, env, releaseID, updatedBy, correlationID string) error
}

// PromotionGitOpsPusher abstracts manifest push to gitops repo (DIP).
type PromotionGitOpsPusher interface {
	Push(ctx context.Context, opts GitOpsPushOpts) (string, error)
}

// PromotionGitOpsReader abstracts manifest retrieval from git (DIP).
type PromotionGitOpsReader interface {
	GetManifestsAtCommit(ctx context.Context, tenantID, projectID, env, commitSHA string) ([]byte, error)
}

// PromotionSyncClient abstracts ArgoCD sync operations (DIP).
type PromotionSyncClient interface {
	TriggerSync(ctx context.Context, appName string) error
}

// PromotionOrchestratorDeps contains all dependencies for the promotion orchestrator.
type PromotionOrchestratorDeps struct {
	Releases   PromotionReleaseStore
	EnvTargets PromotionEnvTargetStore
	GitReader  PromotionGitOpsReader
	GitPusher  PromotionGitOpsPusher
	Sync       PromotionSyncClient
	Locker     EnvironmentLocker
	BaseDomain string
	Logger     *slog.Logger
}

// PromotionRequest contains the inputs for a promotion operation.
type PromotionRequest struct {
	TenantID      string
	ProjectID     string
	ProjectName   string
	FromEnv       string
	ToEnv         string
	CorrelationID string
	UserID        string
}

// PromotionResult contains the outcome of a promotion operation.
type PromotionResult struct {
	PromotionID    string
	SourceRelease  *db.Release
	NewRelease     *db.Release
	FromEnv        string
	ToEnv          string
	URL            string
}

// PromotionOrchestrator coordinates the release promotion workflow.
type PromotionOrchestrator struct {
	deps PromotionOrchestratorDeps
}

// NewPromotionOrchestrator creates a new PromotionOrchestrator.
func NewPromotionOrchestrator(deps PromotionOrchestratorDeps) *PromotionOrchestrator {
	return &PromotionOrchestrator{deps: deps}
}

// Promote executes the promotion workflow:
// 1. Validate from != to
// 2. Validate source environment has an ACTIVE release
// 3. Lock target environment
// 4. Fetch manifests from source release
// 5. Push manifests to target environment path in gitops
// 6. Create new release in target environment with PENDING status
// 7. Trigger ArgoCD sync (release activation happens via sync callback)
func (o *PromotionOrchestrator) Promote(ctx context.Context, req PromotionRequest) (*PromotionResult, error) {
	logger := o.deps.Logger.With(
		"tenant_id", req.TenantID,
		"project_id", req.ProjectID,
		"from_env", req.FromEnv,
		"to_env", req.ToEnv,
		"correlation_id", req.CorrelationID,
	)

	// Validate from != to
	if req.FromEnv == req.ToEnv {
		return nil, loopzeroerrors.ErrSameEnvironment
	}

	// Validate source environment has an ACTIVE release (outside lock — read-only)
	sourceRelease, err := o.deps.Releases.GetActiveByEnv(ctx, req.TenantID, req.ProjectID, req.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("promote: get active release from %s: %w", req.FromEnv, err)
	}
	if sourceRelease == nil {
		return nil, loopzeroerrors.ErrNoActiveRelease
	}

	if sourceRelease.GitCommit == "" {
		return nil, fmt.Errorf("promote: source release %s has no git commit", sourceRelease.ID)
	}

	// Fetch manifests from source release (outside lock — read-only on git)
	manifests, err := o.deps.GitReader.GetManifestsAtCommit(ctx, req.TenantID, req.ProjectID, req.FromEnv, sourceRelease.GitCommit)
	if err != nil {
		return nil, fmt.Errorf("promote: fetch manifests from %s: %w", req.FromEnv, err)
	}

	promotionID := "promo-" + id.GenerateReleaseID()
	var newRelease *db.Release

	// Lock target environment and create the new release
	err = o.deps.Locker.WithLock(ctx, req.TenantID, req.ProjectID, req.ToEnv, func(tx db.DBTX) error {
		// Create new release in target environment
		newRelease = &db.Release{
			ID:            "rel-" + id.GenerateReleaseID(),
			BuildID:       sourceRelease.BuildID,
			TenantID:      req.TenantID,
			ProjectID:     req.ProjectID,
			Environment:   req.ToEnv,
			Status:        "PENDING",
			CorrelationID: req.CorrelationID,
			CreatedBy:     req.UserID,
			Apps:          sourceRelease.Apps,
			Artifacts:     sourceRelease.Artifacts,
		}

		if err := o.deps.Releases.Create(ctx, tx, newRelease); err != nil {
			return fmt.Errorf("promote: create release in %s: %w", req.ToEnv, err)
		}

		// Set desired release pointer for target environment
		if err := o.deps.EnvTargets.SetDesired(ctx, tx, req.TenantID, req.ProjectID, req.ToEnv, newRelease.ID, req.UserID, req.CorrelationID); err != nil {
			return fmt.Errorf("promote: set desired release in %s: %w", req.ToEnv, err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Push manifests to target environment path in gitops
	commitSHA, err := o.deps.GitPusher.Push(ctx, GitOpsPushOpts{
		TenantID:      req.TenantID,
		ProjectID:     req.ProjectID,
		Environment:   req.ToEnv,
		Manifests:     string(manifests),
		BuildID:       sourceRelease.BuildID,
		ReleaseID:     newRelease.ID,
		CorrelationID: req.CorrelationID,
	})
	if err != nil {
		return nil, fmt.Errorf("promote: push manifests to %s: %w", req.ToEnv, err)
	}

	logger.Info("promotion manifests pushed",
		"source_release", sourceRelease.ID,
		"new_release", newRelease.ID,
		"commit_sha", commitSHA,
	)

	// Trigger ArgoCD sync — the sync callback will activate the release
	appName := fmt.Sprintf("%s-%s", req.TenantID, req.ToEnv)
	if err := o.deps.Sync.TriggerSync(ctx, appName); err != nil {
		logger.Error("promote: sync trigger failed — release will be activated by convergence loop",
			"app_name", appName,
			"error", err,
		)
		// Do not fail the promotion — the convergence loop will pick it up
	}

	url := ""
	if o.deps.BaseDomain != "" {
		url = fmt.Sprintf("https://%s-%s.%s", req.ProjectName, req.ToEnv, o.deps.BaseDomain)
	}

	logger.Info("promotion completed",
		"promotion_id", promotionID,
		"source_release", sourceRelease.ID,
		"new_release", newRelease.ID,
		"from_env", req.FromEnv,
		"to_env", req.ToEnv,
	)

	return &PromotionResult{
		PromotionID:   promotionID,
		SourceRelease: sourceRelease,
		NewRelease:    newRelease,
		FromEnv:       req.FromEnv,
		ToEnv:         req.ToEnv,
		URL:           url,
	}, nil
}
