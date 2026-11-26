// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scaling

import (
	"context"
	"errors"
	"fmt"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
)

// StoredScaling overlays the saved scaling configuration of an environment on
// the apps a build declared, by the rule a deploy applies (applyScalingRow).
//
// Every path that renders the manifests of a running release again goes
// through it. One that renders the build's apps as they are writes the build's
// replica count to the GitOps repository and undoes a scale made since.
type StoredScaling struct {
	Configs ScalingConfigReader
}

// Apply returns a copy of apps with the saved config of each app that has one.
func (s StoredScaling) Apply(ctx context.Context, tenantID, project, env string, apps []db.BuildAppSpec) ([]db.BuildAppSpec, error) {
	scaled := make([]db.BuildAppSpec, len(apps))
	for i := range apps {
		app, _, err := s.ApplyToApp(ctx, tenantID, project, env, apps[i])
		if err != nil {
			return nil, err
		}
		scaled[i] = app
	}
	return scaled, nil
}

// ApplyToApp returns app with its saved config, and whether one is saved.
func (s StoredScaling) ApplyToApp(ctx context.Context, tenantID, project, env string, app db.BuildAppSpec) (db.BuildAppSpec, bool, error) {
	row, err := s.Configs.GetByApp(ctx, tenantID, project, env, app.Name)
	if errors.Is(err, store.ErrScalingConfigNotFound) {
		return app, false, nil
	}
	if err != nil {
		return app, false, fmt.Errorf("scaling: read config of app %q: %w", app.Name, err)
	}
	scaled := applyScalingRow(deploycontracts.AppSpecFromStored(app), row)
	app.Replicas = scaled.Replicas
	app.Autoscaling = nil
	if scaled.Autoscaling != nil {
		app.Autoscaling = &db.BuildAppAutoscaling{
			MinReplicas:  scaled.Autoscaling.MinReplicas,
			MaxReplicas:  scaled.Autoscaling.MaxReplicas,
			TargetCPU:    scaled.Autoscaling.TargetCPU,
			TargetMemory: scaled.Autoscaling.TargetMemory,
		}
	}
	return app, true, nil
}
