// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/services"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// BuildOrchestratorAdapter implements routes.BuildOrchestratorService by
// delegating to the real services.BuildOrchestrator. It translates between
// route-level types and service-level types.
type BuildOrchestratorAdapter struct {
	Orch       *services.BuildOrchestrator
	BuildStore db.BuildStore
}

func (a *BuildOrchestratorAdapter) SubmitBuild(ctx context.Context, req routes.SubmitBuildInput) (*routes.SubmitBuildResult, error) {
	var apps []services.AppSpec
	for _, app := range req.Apps {
		appType := app.Type
		if appType == "" {
			appType = "server"
		}
		appPort := app.Port
		if appPort == 0 && appType == "server" {
			appPort = 8080
		}
		spec := services.AppSpec{
			Name:      app.Name,
			Type:      appType,
			Port:      appPort,
			Path:      app.Path,
			Framework: app.Framework,
			Build:     app.Build,
			Rendering: app.Rendering,
			Secrets:   app.Secrets,
			// B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059): forward scaling
			// config to the orchestrator so plan caps and mutual exclusion
			// can be enforced at the authoritative gate.
			Replicas: app.Replicas,
		}
		if app.Autoscaling != nil {
			spec.Autoscaling = &services.AppAutoscalingSpec{
				MinReplicas: app.Autoscaling.Min,
				MaxReplicas: app.Autoscaling.Max,
				TargetCPU:   app.Autoscaling.TargetCPU,
			}
		}
		apps = append(apps, spec)
	}

	resp, err := a.Orch.SubmitBuild(ctx, services.SubmitBuildRequest{
		TenantID:      req.TenantID,
		ProjectID:     req.ProjectID,
		Environment:   req.Environment,
		CommitSHA:     req.CommitSHA,
		NoCache:       req.NoCache,
		Apps:          apps,
		CreatedBy:     req.CreatedBy,
		CorrelationID: req.CorrelationID,
	})
	if err != nil {
		return nil, err
	}

	return &routes.SubmitBuildResult{
		BuildID:   resp.BuildID,
		StreamURL: resp.StreamURL,
	}, nil
}

func (a *BuildOrchestratorAdapter) GetBuildStatus(ctx context.Context, buildID string) (*routes.BuildStatusResponse, error) {
	build, err := a.BuildStore.GetByID(ctx, buildID)
	if err != nil {
		return nil, err
	}
	resp := &routes.BuildStatusResponse{
		BuildID:         build.ID,
		ProjectID:       build.ProjectID,
		Environment:     build.Environment,
		Status:          build.Status,
		CommitSHA:       build.CommitSHA,
		CreatedBy:       build.CreatedBy,
		CreatedAt:       build.CreatedAt.UTC().Format(time.RFC3339),
		DurationSeconds: build.DurationSeconds,
		ErrorMessage:    build.ErrorMessage,
	}
	if build.StartedAt != nil {
		resp.StartedAt = build.StartedAt.UTC().Format(time.RFC3339)
	}
	if build.FinishedAt != nil {
		resp.FinishedAt = build.FinishedAt.UTC().Format(time.RFC3339)
	}
	// Populate apps from stored spec
	apps, _ := a.BuildStore.GetBuildApps(ctx, buildID)
	for _, app := range apps {
		resp.Apps = append(resp.Apps, routes.BuildAppInfo{
			Name: app.Name,
			Type: app.Type,
		})
	}
	return resp, nil
}

func (a *BuildOrchestratorAdapter) OnBuildCompleted(ctx context.Context, buildID string, result routes.BuildCallbackInput) error {
	var apps []services.AppRelease
	for _, app := range result.Apps {
		apps = append(apps, services.AppRelease{
			Name:        app.Name,
			ImageDigest: app.ImageDigest,
			URL:         app.URL,
			Type:        app.Type,
			DurationMs:  app.DurationMs,
		})
	}
	return a.Orch.OnBuildCompleted(ctx, buildID, services.BuildCallbackRequest{
		Status:    result.Status,
		Error:     result.Error,
		Apps:      apps,
		Manifests: result.Manifests,
	})
}

func (a *BuildOrchestratorAdapter) CancelBuild(ctx context.Context, buildID string) error {
	return a.Orch.CancelBuild(ctx, buildID)
}

func (a *BuildOrchestratorAdapter) IsBuildTerminal(ctx context.Context, buildID string) (bool, error) {
	build, err := a.BuildStore.GetByID(ctx, buildID)
	if err != nil {
		return false, err
	}
	return statemachine.IsBuildTerminal(statemachine.State(build.Status)), nil
}

// GetBuildTenantID returns the tenant that owns a build (SEC-4: authorization check).
func (a *BuildOrchestratorAdapter) GetBuildTenantID(ctx context.Context, buildID string) (string, error) {
	build, err := a.BuildStore.GetByID(ctx, buildID)
	if err != nil {
		return "", err
	}
	return build.TenantID, nil
}
