// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package buildsadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/builds/buildshttp"

	"github.com/getloopscale/loop-runtime/api/internal/builds"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"

	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// BuildOrchestratorAdapter implements routes.BuildOrchestratorService by
// delegating to the real build.BuildOrchestrator. It translates between
// route-level types and service-level types.
type BuildOrchestratorAdapter struct {
	Orch       *builds.BuildOrchestrator
	BuildStore interface {
		buildByID
		tenantBuildByID
		buildApps
	}
}

func (a *BuildOrchestratorAdapter) SubmitBuild(ctx context.Context, req buildshttp.SubmitBuildInput) (*buildshttp.SubmitBuildResult, error) {
	apps := appSpecsFromInput(req.Apps)

	resp, err := a.Orch.SubmitBuild(ctx, deploycontracts.SubmitBuildRequest{
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

	return &buildshttp.SubmitBuildResult{
		BuildID:   resp.BuildID,
		StreamURL: resp.StreamURL,
	}, nil
}

func (a *BuildOrchestratorAdapter) GetBuildStatus(ctx context.Context, buildID string) (*buildshttp.BuildStatusResponse, error) {
	build, err := a.BuildStore.GetByID(ctx, buildID)
	if err != nil {
		return nil, err
	}
	resp := &buildshttp.BuildStatusResponse{
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
	// Populate apps from stored spec. A failed read is an error, not an empty
	// app list: the response would otherwise assert something unobserved.
	apps, err := a.BuildStore.GetBuildApps(ctx, buildID)
	if err != nil {
		return nil, fmt.Errorf("build status %s: read apps: %w", buildID, err)
	}
	for _, app := range apps {
		resp.Apps = append(resp.Apps, buildshttp.BuildAppInfo{
			Name: app.Name,
			Type: app.Type,
		})
	}
	return resp, nil
}

func (a *BuildOrchestratorAdapter) CancelBuild(ctx context.Context, buildID string) error {
	return a.Orch.CancelBuild(ctx, buildID)
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
func (a *BuildOrchestratorAdapter) GetRedeploySource(ctx context.Context, tenantID, buildID string) (*buildshttp.RedeploySource, error) {
	build, err := a.BuildStore.GetByIDForTenant(ctx, tenantID, buildID)
	if err != nil {
		return nil, err
	}
	if !statemachine.IsBuildPhaseTerminal(statemachine.BuildPhase(build.Status)) {
		return nil, loopzeroerrors.NewLoopZeroError(
			"BUILD_NOT_TERMINAL",
			"Cannot redeploy a build that is still in progress. Wait for it to finish first.")
	}
	specs, specErr := a.BuildStore.GetBuildApps(ctx, buildID)
	if specErr != nil {
		return nil, specErr
	}
	apps := make([]buildshttp.AppInput, 0, len(specs))
	for _, s := range specs {
		ai := buildshttp.AppInput{
			Name:    s.Name,
			Type:    s.Type,
			Port:    s.Port,
			Path:    s.Path,    // T0.2: now persisted → redeploy preserves monorepo path
			Build:   s.Build,   // T0.2: now persisted → redeploy preserves build mode
			Start:   s.Start,   // T0.2: now persisted → redeploy preserves start command
			Runtime: s.Runtime, // loopzero#185: redeploy MUST preserve services.json runtime (F-dom-2) —
			//                       adapter was silently dropping it → empty runtime → unsupportedStrategy
			//                       → "Failed to build manifests with custom domains" → SYNC_FAILED.
			Rendering: s.Rendering,
			Secrets:   s.Secrets,
			Replicas:  s.Replicas,
		}
		if s.Autoscaling != nil {
			ai.Autoscaling = &buildshttp.AppAutoscalingInput{
				Min:       s.Autoscaling.MinReplicas,
				Max:       s.Autoscaling.MaxReplicas,
				TargetCPU: s.Autoscaling.TargetCPU,
			}
		}
		apps = append(apps, ai)
	}
	return &buildshttp.RedeploySource{
		ProjectID:   build.ProjectID,
		Environment: build.Environment,
		CommitSHA:   build.CommitSHA,
		Apps:        apps,
	}, nil
}

// The port this adapter satisfies, asserted at compile time.
//
// B-058: the package is called `adapters` and its admission criterion was "it is an adapter" —
// which refuses nothing. Measured on 2026-08-18: of the nine adapters, five NAMED their port in
// prose and only ONE verified it. Prose does not compile; this line does, and it is what lets
// the criterion say no.
var _ buildshttp.BuildOrchestratorService = (*BuildOrchestratorAdapter)(nil)

// appSpecsFromInput translates route-level app inputs into the orchestrator's
// AppSpec. An app with no type is a server, and a server with no port listens
// on 8080; other types keep the port they declared (a worker has none).
func appSpecsFromInput(in []buildshttp.AppInput) []deploycontracts.AppSpec {
	var apps []deploycontracts.AppSpec
	for _, app := range in {
		appType := app.Type
		if appType == "" {
			appType = "server"
		}
		appPort := app.Port
		if appPort == 0 && appType == "server" {
			appPort = 8080
		}
		spec := deploycontracts.AppSpec{
			Name:      app.Name,
			Type:      appType,
			Port:      appPort,
			Path:      app.Path,
			Runtime:   app.Runtime, // F-dom-2: services.json runtime is the build-strategy key
			Build:     app.Build,
			Start:     app.Start, // T0.2: declared start → canonical Dockerfile CMD
			Rendering: app.Rendering,
			Secrets:   app.Secrets,
			// B15: forward scaling config so plan caps and mutual exclusion
			// are enforced at the authoritative gate.
			Replicas: app.Replicas,
		}
		if app.Autoscaling != nil {
			spec.Autoscaling = &deploycontracts.AppAutoscalingSpec{
				MinReplicas: app.Autoscaling.Min,
				MaxReplicas: app.Autoscaling.Max,
				TargetCPU:   app.Autoscaling.TargetCPU,
			}
		}
		apps = append(apps, spec)
	}
	return apps
}
