// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// ScalingResolver — closes B40 gap: app_scaling_config table is read by the
// deploy path. See loopzero-scale-end-to-end-plan (T1.1, D1, D2).
//
// Precedence (D1): if app_scaling_config has a row for (tenant, project, env, app),
// that row OVERRIDES the value from loopzero.yaml. Otherwise loopzero.yaml is preserved
// (backward-compat).

// Package scaling is the scaling bounded context (replica/HPA resolution, plan-cap enforcement). M9 Step 6.
package scaling

import (
	"context"
	"errors"
	"fmt"

	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// ScalingResolver merges yaml-supplied AppSpecs with persisted scaling
// configuration from the DB. Used by build_orchestrator_submit before
// plan-cap enforcement and BuildAppSpec persistence.
type ScalingResolver interface {
	Resolve(
		ctx context.Context,
		tenantID, projectID, environment string,
		fromYAML []deploycontracts.AppSpec,
	) ([]deploycontracts.AppSpec, error)
}

// ScalingConfigReader is the one scaling-store read the resolver makes (ISP —
// store.ScalingStore also carries the writes the scaling routes use).
type ScalingConfigReader interface {
	GetByApp(ctx context.Context, tenantID, projectID, environment, appName string) (*store.ScalingConfig, error)
}

// dbScalingResolver implements ScalingResolver via a ScalingConfigReader.
type dbScalingResolver struct {
	store ScalingConfigReader
}

// NewDBScalingResolver constructs a resolver backed by the given store. A nil
// store is refused: a resolver without one would deploy the yaml's scaling and
// silently ignore every persisted scaling config.
func NewDBScalingResolver(store ScalingConfigReader) (ScalingResolver, error) {
	if store == nil {
		return nil, &loopzeroerrors.LoopZeroError{Code: loopzeroerrors.ErrInternalError.Code, Message: "scaling resolver: store is required"}
	}
	return &dbScalingResolver{store: store}, nil
}

// Resolve walks fromYAML, queries the store for each app, and overrides
// scaling fields when a DB row exists. Order is preserved — output[i]
// corresponds to input[i].
//
// Branches per row state:
//   - ScaleToZero=true       → KEDA path (Replicas=0, Autoscaling={Min,Max})
//   - MinReplicas==MaxReplicas → fixed (Replicas=Min, Autoscaling=nil)
//   - MinReplicas<MaxReplicas  → HPA (Replicas=Min as baseline, Autoscaling={Min,Max,CPU})
func (r *dbScalingResolver) Resolve(
	ctx context.Context,
	tenantID, projectID, environment string,
	fromYAML []deploycontracts.AppSpec,
) ([]deploycontracts.AppSpec, error) {
	if len(fromYAML) == 0 {
		return fromYAML, nil
	}

	resolved := make([]deploycontracts.AppSpec, len(fromYAML))
	for i, app := range fromYAML {
		row, err := r.store.GetByApp(ctx, tenantID, projectID, environment, app.Name)
		if err != nil {
			if errors.Is(err, store.ErrScalingConfigNotFound) {
				// No DB override — preserve yaml.
				resolved[i] = app
				continue
			}
			return nil, fmt.Errorf("scaling resolver: get app %q: %w", app.Name, err)
		}
		resolved[i] = applyScalingRow(app, row)
	}
	return resolved, nil
}

// applyScalingRow returns a copy of app with scaling fields overridden by row.
// Other fields (Type, Port, Secrets, etc.) are preserved verbatim.
func applyScalingRow(app deploycontracts.AppSpec, row *store.ScalingConfig) deploycontracts.AppSpec {
	switch {
	case row.ScaleToZero:
		// KEDA scale-to-zero (D4): Deployment.spec.replicas=0, ScaledObject manages.
		app.Replicas = 0
		app.Autoscaling = &deploycontracts.AppAutoscalingSpec{
			MinReplicas:  int(row.MinReplicas),
			MaxReplicas:  int(row.MaxReplicas),
			TargetCPU:    resolveTarget(row.TargetCPUPct),
			TargetMemory: resolveTarget(row.TargetMemoryPct),
		}
	case row.MinReplicas == row.MaxReplicas:
		// Fixed replicas: HPA not needed.
		app.Replicas = int(row.MinReplicas)
		app.Autoscaling = nil
	default:
		// HPA: Deployment.spec.replicas=Min as baseline, HPA scales to Max.
		app.Replicas = int(row.MinReplicas)
		app.Autoscaling = &deploycontracts.AppAutoscalingSpec{
			MinReplicas:  int(row.MinReplicas),
			MaxReplicas:  int(row.MaxReplicas),
			TargetCPU:    resolveTarget(row.TargetCPUPct),
			TargetMemory: resolveTarget(row.TargetMemoryPct),
		}
	}
	return app
}

// resolveTarget is a saved utilization target as the renderer reads it: 0 when
// none is saved, which the renderer takes as its default for CPU and as no
// metric for memory.
func resolveTarget(pct *int32) int {
	if pct == nil {
		return 0
	}
	return int(*pct)
}
