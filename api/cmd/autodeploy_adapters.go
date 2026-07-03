// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/getloopscale/loopzero/api/internal/db/store"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/github"
	"github.com/getloopscale/loopzero/api/internal/k8s"
	"github.com/getloopscale/loopzero/api/internal/services"
)

// buildLookupAdapter bridges db.BuildStore → github.BuildLookup.
type buildLookupAdapter struct {
	store db.BuildStore
}

func (a *buildLookupAdapter) GetCommitSHA(ctx context.Context, buildID string) (string, error) {
	build, err := a.store.GetByID(ctx, buildID)
	if err != nil || build == nil {
		return "", err
	}
	return build.CommitSHA, nil
}

// repoLookupAdapter bridges store.PostgresRepositoryStore → github.RepositoryLookup.
type repoLookupAdapter struct {
	store *store.PostgresRepositoryStore
}

func (a *repoLookupAdapter) GetByProject(ctx context.Context, tenantID, projectName string) (string, int64, error) {
	r, err := a.store.GetByProject(ctx, tenantID, projectName)
	if err != nil || r == nil {
		return "", 0, err
	}
	return r.RepoFullName, r.InstallationID, nil
}

// buildSubmitterAdapter bridges github.BuildSubmitter → services.BuildOrchestrator.
type buildSubmitterAdapter struct {
	orchestrator *services.BuildOrchestrator
	logger       *slog.Logger
}

func (a *buildSubmitterAdapter) SubmitBuildFromWebhook(ctx context.Context, req github.WebhookBuildRequest) error {
	if req.LoopZeroYaml == nil {
		return fmt.Errorf("no loopzero.yaml parsed")
	}

	// Sponsor LIVE-caught 2026-06-18: this conversion previously lived
	// inline here AND in services/preview_service.go appsFromLoopZeroYaml,
	// with a comment "mirror of autodeploy_adapters.go" admitting the
	// duplication. Canonical 2026-06-16 added `start` field extraction
	// only to this path — preview path silently kept losing it, so PR
	// previews failed at generate-dockerfile time while push deploys
	// succeeded against the same loopzero.yaml. Single source of truth now
	// lives in services.ConvertLoopZeroYamlApps (with hermetic regression
	// locks for every extracted field).
	apps := services.ConvertLoopZeroYamlApps(req.LoopZeroYaml.Apps)

	a.logger.Info("auto-deploy: submitting build",
		"project", req.ProjectName,
		"environment", req.Environment,
		"sha", req.CommitSHA,
		"apps", len(apps),
	)

	_, err := a.orchestrator.SubmitBuild(ctx, services.SubmitBuildRequest{
		TenantID:      req.TenantID,
		ProjectID:     req.ProjectName,
		Environment:   req.Environment,
		CommitSHA:     req.CommitSHA,
		Apps:          apps,
		CreatedBy:     fmt.Sprintf("github-push:%s", req.SenderLogin),
		CorrelationID: fmt.Sprintf("gh-push-%s-%s", req.RepoFullName, req.CommitSHA[:8]),
	})
	if err != nil {
		return fmt.Errorf("build submit failed: %w", err)
	}

	a.logger.Info("auto-deploy: build submitted successfully",
		"project", req.ProjectName,
		"sha", req.CommitSHA,
	)
	return nil
}

// previewAppsLookupAdapter implements services.PreviewBuildAppsLookup by
// copying Apps from the most recent SUCCEEDED build of the (tenant, project).
// J-23 LIVE catch 2026-06-07: without this, preview builds fail with
// "WorkflowSpec.Plan must be non-nil" because PR webhooks don't ship
// parsed loopzero.yaml (only push webhooks do via WebhookBuildRequest).
type previewAppsLookupAdapter struct {
	builds db.BuildStore
}

func (a *previewAppsLookupAdapter) LatestSucceededApps(ctx context.Context, tenantID, projectID string) ([]services.AppSpec, error) {
	// ListByProject newest first; iterate to find latest SUCCEEDED.
	const lookbackLimit = 20
	builds, err := a.builds.ListByProject(ctx, tenantID, projectID, lookbackLimit, 0)
	if err != nil {
		return nil, fmt.Errorf("preview apps lookup: list builds: %w", err)
	}
	var sourceBuildID string
	for _, b := range builds {
		if b.Status == "SUCCEEDED" {
			sourceBuildID = b.ID
			break
		}
	}
	if sourceBuildID == "" {
		return nil, nil // honest empty — preview service logs the hint
	}
	dbApps, err := a.builds.GetBuildApps(ctx, sourceBuildID)
	if err != nil {
		return nil, fmt.Errorf("preview apps lookup: get apps for %s: %w", sourceBuildID, err)
	}
	out := make([]services.AppSpec, 0, len(dbApps))
	for _, da := range dbApps {
		app := services.AppSpec{
			Name:      da.Name,
			Type:      da.Type,
			Rendering: da.Rendering,
			Port:      da.Port,
			Secrets:   da.Secrets,
			Replicas:  da.Replicas,
		}
		if da.Autoscaling != nil {
			app.Autoscaling = &services.AppAutoscalingSpec{
				MinReplicas: da.Autoscaling.MinReplicas,
				MaxReplicas: da.Autoscaling.MaxReplicas,
				TargetCPU:   da.Autoscaling.TargetCPU,
			}
		}
		out = append(out, app)
	}
	return out, nil
}

// previewCRCreatorAdapter bridges services.PreviewCRCreator (services
// package's spec type) to k8s.PreviewCRManager (k8s package's spec
// type). Two spec types exist to avoid the k8s↔services import cycle;
// fields are identical so the conversion is a flat copy.
type previewCRCreatorAdapter struct {
	mgr *k8s.PreviewCRManager
}

func (a *previewCRCreatorAdapter) EnsurePreviewCR(ctx context.Context, spec services.PreviewCRSpec) error {
	return a.mgr.EnsurePreviewCR(ctx, k8s.PreviewCRSpec{
		Name:      spec.Name,
		TenantID:  spec.TenantID,
		ProjectID: spec.ProjectID,
		PRNumber:  spec.PRNumber,
		Branch:    spec.Branch,
		ReleaseID: spec.ReleaseID,
		ExpiresAt: spec.ExpiresAt,
	})
}

func (a *previewCRCreatorAdapter) DeletePreviewCR(ctx context.Context, tenantID, projectID string, prNumber int) error {
	return a.mgr.DeletePreviewCR(ctx, tenantID, projectID, prNumber)
}
