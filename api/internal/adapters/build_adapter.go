// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"
	"time"

	"github.com/getloopscale/loopzero/api/internal/services/build"

	"github.com/getloopscale/loopzero/api/internal/services/shared"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/routes"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// BuildOrchestratorAdapter implements routes.BuildOrchestratorService by
// delegating to the real build.BuildOrchestrator. It translates between
// route-level types and service-level types.
type BuildOrchestratorAdapter struct {
	Orch       *build.BuildOrchestrator
	BuildStore db.BuildStore
}

func (a *BuildOrchestratorAdapter) SubmitBuild(ctx context.Context, req routes.SubmitBuildInput) (*routes.SubmitBuildResult, error) {
	var apps []shared.AppSpec
	for _, app := range req.Apps {
		appType := app.Type
		if appType == "" {
			appType = "server"
		}
		appPort := app.Port
		if appPort == 0 && appType == "server" {
			appPort = 8080
		}
		spec := shared.AppSpec{
			Name:      app.Name,
			Type:      appType,
			Port:      appPort,
			Path:      app.Path,
			Runtime:   app.Runtime, // F-dom-2: services.json runtime is the build-strategy key
			Build:     app.Build,
			Start:     app.Start, // T0.2: declared start → canonical Dockerfile CMD
			Rendering: app.Rendering,
			Secrets:   app.Secrets,
			// B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059): forward scaling
			// config to the orchestrator so plan caps and mutual exclusion
			// can be enforced at the authoritative gate.
			Replicas: app.Replicas,
		}
		if app.Autoscaling != nil {
			spec.Autoscaling = &shared.AppAutoscalingSpec{
				MinReplicas: app.Autoscaling.Min,
				MaxReplicas: app.Autoscaling.Max,
				TargetCPU:   app.Autoscaling.TargetCPU,
			}
		}
		apps = append(apps, spec)
	}

	resp, err := a.Orch.SubmitBuild(ctx, shared.SubmitBuildRequest{
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
	var apps []shared.AppRelease
	for _, app := range result.Apps {
		apps = append(apps, shared.AppRelease{
			Name:        app.Name,
			ImageDigest: app.ImageDigest,
			URL:         app.URL,
			Type:        app.Type,
			DurationMs:  app.DurationMs,
		})
	}
	return a.Orch.OnBuildCompleted(ctx, buildID, shared.BuildCallbackRequest{
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
	return statemachine.IsBuildPhaseTerminal(statemachine.BuildPhase(build.Status)), nil
}

// GetBuildTenantID returns the tenant that owns a build (SEC-4: authorization check).
func (a *BuildOrchestratorAdapter) GetBuildTenantID(ctx context.Context, buildID string) (string, error) {
	build, err := a.BuildStore.GetByID(ctx, buildID)
	if err != nil {
		return "", err
	}
	return build.TenantID, nil
}

// GetRedeploySource — Plan J-17 T6.4. Returns the params needed to
// resubmit the source build as a redeploy. Verifies tenant ownership
// AND that the source build reached a terminal state (otherwise the
// dashboard would race the in-flight build's own concurrent-build
// guard).
//
// The source's apps_spec is converted back to AppInput. Path / Build /
// Runtime / Start ARE recovered from the persisted apps_spec (T0.2/T0.1 —
// the schema persists them). The legacy `framework` field was removed
// (simplify-deploy-loopzerokit-runtime-wiring / F-dom-2): the build strategy is
// keyed on the persisted `runtime` (python|node).
func (a *BuildOrchestratorAdapter) GetRedeploySource(ctx context.Context, tenantID, buildID string) (*routes.RedeploySource, error) {
	build, err := a.BuildStore.GetByIDForTenant(ctx, tenantID, buildID)
	if err != nil {
		return nil, err
	}
	if !statemachine.IsBuildPhaseTerminal(statemachine.BuildPhase(build.Status)) {
		return nil, loopzeroerrors.NewLoopZeroError(
			"BUILD_NOT_TERMINAL",
			"Cannot redeploy a build that is still in progress. Wait for it to finish first.",
			409,
		)
	}
	specs, specErr := a.BuildStore.GetBuildApps(ctx, buildID)
	if specErr != nil {
		return nil, specErr
	}
	apps := make([]routes.AppInput, 0, len(specs))
	for _, s := range specs {
		ai := routes.AppInput{
			Name:      s.Name,
			Type:      s.Type,
			Port:      s.Port,
			Path:      s.Path,  // T0.2: now persisted → redeploy preserves monorepo path
			Build:     s.Build, // T0.2: now persisted → redeploy preserves build mode
			Start:     s.Start, // T0.2: now persisted → redeploy preserves start command
			Rendering: s.Rendering,
			Secrets:   s.Secrets,
			Replicas:  s.Replicas,
		}
		if s.Autoscaling != nil {
			ai.Autoscaling = &routes.AppAutoscalingInput{
				Min:       s.Autoscaling.MinReplicas,
				Max:       s.Autoscaling.MaxReplicas,
				TargetCPU: s.Autoscaling.TargetCPU,
			}
		}
		apps = append(apps, ai)
	}
	return &routes.RedeploySource{
		ProjectID:   build.ProjectID,
		Environment: build.Environment,
		CommitSHA:   build.CommitSHA,
		Apps:        apps,
	}, nil
}
