// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statusadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/getloopscale/loopzero/api/internal/status/statushttp"

	"github.com/getloopscale/loopzero/api/internal/db"
)

// StatusBuildStoreAdapter implements routes.StatusBuildStore using db.BuildStore.
// The status handler needs only a subset of build fields for the project status response.
type StatusBuildStoreAdapter struct {
	Store projectBuilds
}

func (a *StatusBuildStoreAdapter) ListByProject(ctx context.Context, tenantID, projectID string, limit, offset int) ([]statushttp.StatusBuild, error) {
	builds, err := a.Store.ListByProject(ctx, tenantID, projectID, limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]statushttp.StatusBuild, len(builds))
	for i, b := range builds {
		scaling, serr := extractScalingFromAppsSpec(b.AppsSpec)
		if serr != nil {
			slog.WarnContext(ctx, "build status: apps_spec does not decode; scaling omitted",
				"build_id", b.ID, "tenant_id", tenantID, "error", serr)
		}
		result[i] = statushttp.StatusBuild{
			ID:              b.ID,
			Environment:     b.Environment,
			Status:          b.Status,
			CommitSHA:       b.CommitSHA,
			CreatedAt:       b.CreatedAt,
			FinishedAt:      b.FinishedAt,
			DurationSeconds: b.DurationSeconds,
			ErrorMessage:    b.ErrorMessage,
			Scaling:         scaling,
		}
	}
	return result, nil
}

// extractScalingFromAppsSpec parses the first app's autoscaling config from the
// build's apps_spec JSON. Returns (nil, nil) when no autoscaling is configured
// and an error when the stored value is not the array of apps it should be.
func extractScalingFromAppsSpec(appsSpec json.RawMessage) (*statushttp.StatusBuildScaling, error) {
	if len(appsSpec) == 0 {
		return nil, nil
	}
	var apps []db.BuildAppSpec
	if err := json.Unmarshal(appsSpec, &apps); err != nil {
		return nil, fmt.Errorf("decode apps_spec: %w", err)
	}
	// Use the first app with autoscaling configured (matches Apps[0] pattern).
	for _, app := range apps {
		if app.Autoscaling != nil && app.Autoscaling.MaxReplicas > 0 {
			return &statushttp.StatusBuildScaling{
				MinReplicas: app.Autoscaling.MinReplicas,
				MaxReplicas: app.Autoscaling.MaxReplicas,
			}, nil
		}
	}
	return nil, nil
}

// The port this adapter satisfies, asserted at compile time.
var _ statushttp.StatusBuildStore = (*StatusBuildStoreAdapter)(nil)
