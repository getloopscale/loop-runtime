// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"fmt"
	"log/slog"

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

// repoLookupAdapter bridges db.PostgresRepositoryStore → github.RepositoryLookup.
type repoLookupAdapter struct {
	store *db.PostgresRepositoryStore
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

	// Convert loopzero.yaml apps to AppSpec
	var apps []services.AppSpec
	for name, appCfg := range req.LoopZeroYaml.Apps {
		app := services.AppSpec{
			Name: name,
		}

		// Extract fields from the map[string]interface{}
		if v, ok := appCfg["type"].(string); ok {
			app.Type = v
		} else {
			app.Type = "server" // default
		}
		if v, ok := appCfg["runtime"].(string); ok {
			app.Runtime = v // F-dom-2: runtime is the build-strategy key (framework removed)
		}
		if v, ok := appCfg["path"].(string); ok {
			app.Path = v
		}
		if v, ok := appCfg["port"].(int); ok {
			app.Port = v
		} else if v, ok := appCfg["port"].(float64); ok {
			app.Port = int(v)
		}
		if v, ok := appCfg["build"].(string); ok {
			app.Build = v
		}
		if v, ok := appCfg["render"].(string); ok {
			app.Rendering = v
		}
		// Canonical 2026-06-16: extract `start` field from loopzero.yaml apps map.
		// The canonical Dockerfile generator (canonical_dockerfile.go) requires
		// an explicit start command — without it, generate-dockerfile fails at
		// build time with "app 'X' has no declared start command". The
		// loopzerokit-example fixture declares `start: loopzerokit start`.
		if v, ok := appCfg["start"].(string); ok {
			app.Start = v
		}

		// Auto-infer: frontend → static
		if app.Type == "frontend" && app.Rendering == "" {
			app.Rendering = "static"
		}

		// B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059): extract scaling config
		// from the webhook-parsed loopzero.yaml so plan-cap enforcement in
		// BuildOrchestrator.SubmitBuild sees the requested values. Without this
		// extraction, `replicas: 99` silently becomes 0 and bypasses the cap.
		if v, ok := appCfg["replicas"].(int); ok {
			app.Replicas = v
		} else if v, ok := appCfg["replicas"].(float64); ok {
			app.Replicas = int(v)
		}
		if asRaw, ok := appCfg["autoscaling"].(map[string]interface{}); ok {
			as := &services.AppAutoscalingSpec{}
			if v, ok := asRaw["min"].(int); ok {
				as.MinReplicas = v
			} else if v, ok := asRaw["min"].(float64); ok {
				as.MinReplicas = int(v)
			}
			if v, ok := asRaw["max"].(int); ok {
				as.MaxReplicas = v
			} else if v, ok := asRaw["max"].(float64); ok {
				as.MaxReplicas = int(v)
			}
			if v, ok := asRaw["targetCPU"].(int); ok {
				as.TargetCPU = v
			} else if v, ok := asRaw["targetCPU"].(float64); ok {
				as.TargetCPU = int(v)
			}
			app.Autoscaling = as
		}

		apps = append(apps, app)
	}

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
