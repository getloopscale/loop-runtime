// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statusadapter

import (
	"context"
	"fmt"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/manifests"
	"github.com/getloopscale/loop-runtime/api/internal/status/statushttp"
)

// buildApps reads the apps a build declared.
type buildApps interface {
	GetBuildApps(ctx context.Context, buildID string) ([]db.BuildAppSpec, error)
}

// savedScaling overlays the saved scaling configuration on a declared app
// (scaling.StoredScaling).
type savedScaling interface {
	ApplyToApp(ctx context.Context, tenantID, project, env string, app db.BuildAppSpec) (db.BuildAppSpec, bool, error)
}

// StatusScaleAdapter reports the scale each app of a running release is
// configured with: the saved scaling configuration where one exists, and what
// the build declared otherwise. It reads what a render of the release would
// use, so the answer follows a scale made after the build.
type StatusScaleAdapter struct {
	Builds buildApps
	Stored savedScaling
}

// DesiredScale implements statushttp.StatusScaleReader.
func (a StatusScaleAdapter) DesiredScale(ctx context.Context, tenantID, projectID, environment, buildID string) ([]statushttp.StatusAppScale, error) {
	apps, err := a.Builds.GetBuildApps(ctx, buildID)
	if err != nil {
		return nil, fmt.Errorf("read apps of build %s: %w", buildID, err)
	}
	scales := make([]statushttp.StatusAppScale, 0, len(apps))
	for _, declared := range apps {
		app, saved, err := a.Stored.ApplyToApp(ctx, tenantID, projectID, environment, declared)
		if err != nil {
			return nil, err
		}
		input := manifests.AppManifestInput{Replicas: app.Replicas}
		if app.Autoscaling != nil {
			input.Autoscaling = &manifests.AutoscalingConfig{
				MinReplicas: app.Autoscaling.MinReplicas,
				MaxReplicas: app.Autoscaling.MaxReplicas,
			}
		}
		scale := statushttp.StatusAppScale{App: app.Name, Replicas: manifests.RenderedReplicas(input), Saved: saved}
		switch {
		case app.Autoscaling != nil:
			scale.Min, scale.Max = app.Autoscaling.MinReplicas, app.Autoscaling.MaxReplicas
			scale.TargetCPU, scale.TargetMemory = app.Autoscaling.TargetCPU, app.Autoscaling.TargetMemory
		case saved:
			scale.Min, scale.Max = scale.Replicas, scale.Replicas
		}
		scales = append(scales, scale)
	}
	return scales, nil
}

var _ statushttp.StatusScaleReader = StatusScaleAdapter{}
