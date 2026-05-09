// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"
	"encoding/json"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/sse"
)

// BuildStoreReaderAdapter implements sse.BuildStoreReader using db.BuildStore.
// The SSE stream handler needs only GetByID returning sse.BuildInfo,
// while db.BuildStore returns the full db.Build struct.
type BuildStoreReaderAdapter struct {
	Store db.BuildStore
}

func (a *BuildStoreReaderAdapter) GetByID(ctx context.Context, id string) (*sse.BuildInfo, error) {
	build, err := a.Store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &sse.BuildInfo{
		ID:       build.ID,
		TenantID: build.TenantID,
		Status:   build.Status,
	}, nil
}

// LogBuildStoreAdapter implements routes.LogBuildStore using db.BuildStore.
// The logs handler needs only build ID, tenant, and status for authorization
// and response metadata.
type LogBuildStoreAdapter struct {
	Store db.BuildStore
}

func (a *LogBuildStoreAdapter) GetByID(ctx context.Context, id string) (*routes.LogBuild, error) {
	build, err := a.Store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &routes.LogBuild{
		ID:       build.ID,
		TenantID: build.TenantID,
		Status:   build.Status,
	}, nil
}

// StatusBuildStoreAdapter implements routes.StatusBuildStore using db.BuildStore.
// The status handler needs only a subset of build fields for the project status response.
type StatusBuildStoreAdapter struct {
	Store db.BuildStore
}

func (a *StatusBuildStoreAdapter) ListByProject(ctx context.Context, tenantID, projectID string, limit, offset int) ([]routes.StatusBuild, error) {
	builds, err := a.Store.ListByProject(ctx, tenantID, projectID, limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]routes.StatusBuild, len(builds))
	for i, b := range builds {
		result[i] = routes.StatusBuild{
			ID:              b.ID,
			Environment:     b.Environment,
			Status:          b.Status,
			CommitSHA:       b.CommitSHA,
			CreatedAt:       b.CreatedAt,
			FinishedAt:      b.FinishedAt,
			DurationSeconds: b.DurationSeconds,
			ErrorMessage:    b.ErrorMessage,
			Scaling:         extractScalingFromAppsSpec(b.AppsSpec),
		}
	}
	return result, nil
}

// extractScalingFromAppsSpec parses the first app's autoscaling config from the
// build's apps_spec JSON. Returns nil if no autoscaling is configured.
func extractScalingFromAppsSpec(appsSpec json.RawMessage) *routes.StatusBuildScaling {
	if len(appsSpec) == 0 {
		return nil
	}
	var apps []db.BuildAppSpec
	if err := json.Unmarshal(appsSpec, &apps); err != nil {
		return nil
	}
	// Use the first app with autoscaling configured (matches Apps[0] pattern).
	for _, app := range apps {
		if app.Autoscaling != nil && app.Autoscaling.MaxReplicas > 0 {
			return &routes.StatusBuildScaling{
				MinReplicas: app.Autoscaling.MinReplicas,
				MaxReplicas: app.Autoscaling.MaxReplicas,
			}
		}
	}
	return nil
}

// BuildListStoreAdapter implements routes.BuildListStore using db.BuildStore.
type BuildListStoreAdapter struct {
	Store db.BuildStore
}

func (a *BuildListStoreAdapter) ListByProjectAndEnv(ctx context.Context, tenantID, projectID, env string, limit, offset int) ([]routes.BuildListItem, int, error) {
	builds, total, err := a.Store.ListByProjectAndEnv(ctx, tenantID, projectID, env, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	result := make([]routes.BuildListItem, len(builds))
	for i, b := range builds {
		result[i] = routes.BuildListItem{
			ID:              b.ID,
			ProjectID:       b.ProjectID,
			Environment:     b.Environment,
			Status:          b.Status,
			CommitSHA:       b.CommitSHA,
			CreatedBy:       b.CreatedBy,
			CreatedAt:       b.CreatedAt,
			StartedAt:       b.StartedAt,
			FinishedAt:      b.FinishedAt,
			DurationSeconds: b.DurationSeconds,
			ErrorMessage:    b.ErrorMessage,
		}
	}
	return result, total, nil
}
