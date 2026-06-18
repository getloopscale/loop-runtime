// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"
	"encoding/json"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/routes"
	"github.com/getloopscale/loopzero/api/internal/sse"
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

// GetTenantForBuild satisfies the aggregator.BuildLookup contract used by
// PostgresDeployTimelineStore (plan T5.1 wiring). Returns the empty
// string when the build is not found OR an error occurs — the caller
// translates that into routes.ErrDeployNotFound (existence-leak guard).
func (a *BuildStoreReaderAdapter) GetTenantForBuild(ctx context.Context, buildID string) (string, error) {
	build, err := a.Store.GetByID(ctx, buildID)
	if err != nil {
		return "", err
	}
	if build == nil {
		return "", nil
	}
	return build.TenantID, nil
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
	if build == nil {
		return nil, nil
	}
	return &routes.LogBuild{
		ID:       build.ID,
		TenantID: build.TenantID,
		Status:   build.Status,
	}, nil
}

// GetByIDForTenant routes to the tenant-scoped DB query — T0.4 of
// deep-review-2026-05-10-remediation-plan (find_sec_idor_buildstore).
func (a *LogBuildStoreAdapter) GetByIDForTenant(ctx context.Context, tenantID, id string) (*routes.LogBuild, error) {
	build, err := a.Store.GetByIDForTenant(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if build == nil {
		return nil, nil
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
			// T5.3 LIVE-bug iter 20 (2026-06-08): parse apps_spec JSONB
			// → []routes.BuildAppInfo. Empty/invalid JSON resolves to nil
			// slice (omitempty drops it from the JSON response). Parse
			// errors are silently swallowed because apps_spec is a
			// best-effort denormalization for display — the build itself
			// is the source of truth, and the dashboard renders empty
			// apps lists gracefully via the appsFromBuild?.length guard.
			Apps: decodeAppsSpecForListing(b.AppsSpec),
		}
	}
	return result, total, nil
}

// decodeAppsSpecForListing parses a build's apps_spec JSONB column into
// the route-level []routes.BuildAppInfo slice for serialization. Returns
// nil on empty/invalid input so the omitempty JSON tag drops the field.
// loopzero.yaml apps schema: [{ "name": "...", "type": "...", "port": N,
// "rendering": "..." }] — we surface only name + type per the dashboard
// Apps card contract (BuildAppInfo in api/internal/routes/builds.go).
func decodeAppsSpecForListing(raw json.RawMessage) []routes.BuildAppInfo {
	if len(raw) == 0 {
		return nil
	}
	var parsed []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil
	}
	if len(parsed) == 0 {
		return nil
	}
	out := make([]routes.BuildAppInfo, len(parsed))
	for i, p := range parsed {
		out[i] = routes.BuildAppInfo{Name: p.Name, Type: p.Type}
	}
	return out
}

// ListEnvsForProject delegates to the underlying db.BuildStore. Post
// plan 2026-06-08-dashboard-screens-review-fixes T2.2 + ADR D3 + EC-2.
// The caller (BuildListHandler) is responsible for the cross-tenant
// gate via ProjectStore.GetByName BEFORE invoking this method.
func (a *BuildListStoreAdapter) ListEnvsForProject(ctx context.Context, tenantID, projectName string) ([]string, error) {
	return a.Store.ListEnvsForProject(ctx, tenantID, projectName)
}
