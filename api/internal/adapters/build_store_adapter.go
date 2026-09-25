// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	buildroutes "github.com/getloopscale/loopzero/api/internal/builds/buildshttp"
	"github.com/getloopscale/loopzero/api/internal/logs/logshttp"
	"github.com/getloopscale/loopzero/api/internal/status/statushttp"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/generated"
	"github.com/getloopscale/loopzero/api/internal/sse"
)

// BuildStoreReaderAdapter implements sse.BuildStoreReader using db.BuildStore.
// The SSE stream handler needs only GetByID returning sse.BuildInfo,
// while db.BuildStore returns the full db.Build struct.
type BuildStoreReaderAdapter struct {
	Store buildByID
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
	Store interface {
		buildByID
		tenantBuildByID
	}
}

func (a *LogBuildStoreAdapter) GetByID(ctx context.Context, id string) (*logshttp.LogBuild, error) {
	build, err := a.Store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if build == nil {
		return nil, nil
	}
	return &logshttp.LogBuild{
		ID:       build.ID,
		TenantID: build.TenantID,
		Status:   build.Status,
	}, nil
}

// GetByIDForTenant routes to the tenant-scoped DB query — T0.4 of
// deep-review-2026-05-10-remediation-plan (find_sec_idor_buildstore).
func (a *LogBuildStoreAdapter) GetByIDForTenant(ctx context.Context, tenantID, id string) (*logshttp.LogBuild, error) {
	build, err := a.Store.GetByIDForTenant(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if build == nil {
		return nil, nil
	}
	return &logshttp.LogBuild{
		ID:       build.ID,
		TenantID: build.TenantID,
		Status:   build.Status,
	}, nil
}

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

// BuildListStoreAdapter implements routes.BuildListStore using db.BuildStore.
type BuildListStoreAdapter struct {
	Store envBuilds
}

func (a *BuildListStoreAdapter) ListByProjectAndEnv(ctx context.Context, tenantID, projectID, env string, limit, offset int) ([]buildroutes.BuildListItem, int, error) {
	builds, total, err := a.Store.ListByProjectAndEnv(ctx, tenantID, projectID, env, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	result := make([]buildroutes.BuildListItem, len(builds))
	for i, b := range builds {
		apps, aerr := decodeAppsSpecForListing(b.AppsSpec)
		if aerr != nil {
			// apps_spec is a display denormalization; the row still lists, but
			// a spec that does not decode is recorded with the build it names
			// rather than shown as "no apps".
			slog.WarnContext(ctx, "build list: apps_spec does not decode; apps omitted",
				"build_id", b.ID, "tenant_id", tenantID, "error", aerr)
		}
		result[i] = buildroutes.BuildListItem{
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
			Apps:            apps,
		}
	}
	return result, total, nil
}

// decodeAppsSpecForListing parses a build's apps_spec JSONB column into
// the route-level []routes.BuildAppInfo slice for serialization. Empty input
// is (nil, nil) so the omitempty JSON tag drops the field; a value that is not
// an array of apps is an error for the caller to record.
// loopzero.yaml apps schema: [{ "name": "...", "type": "...", "port": N,
// "rendering": "..." }] — we surface only name + type per the dashboard
// Apps card contract (BuildAppInfo in api/internal/routes/builds.go).
func decodeAppsSpecForListing(raw json.RawMessage) ([]buildroutes.BuildAppInfo, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var parsed []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode apps_spec: %w", err)
	}
	if len(parsed) == 0 {
		return nil, nil
	}
	out := make([]buildroutes.BuildAppInfo, len(parsed))
	for i, p := range parsed {
		out[i] = buildroutes.BuildAppInfo{Name: p.Name, Type: p.Type}
	}
	return out, nil
}

// ListEnvsForProject delegates to the underlying db.BuildStore. Post
// plan 2026-06-08-dashboard-screens-review-fixes T2.2 + ADR D3 + EC-2.
// The caller (BuildListHandler) is responsible for the cross-tenant
// gate via ProjectStore.GetByName BEFORE invoking this method.
func (a *BuildListStoreAdapter) ListEnvsForProject(ctx context.Context, tenantID, projectName string) ([]string, error) {
	return a.Store.ListEnvsForProject(ctx, tenantID, projectName)
}

// buildStatsSource is the narrow store dependency for the deploys-aggregate
// adapter (ISP) — only the sqlc BuildStatsByDay method, nothing else.
type buildStatsSource interface {
	BuildStatsByDay(ctx context.Context, tenantID string, since time.Time) ([]generated.BuildStatsByDayRow, error)
}

// BuildStatsStoreAdapter adapts the sqlc BuildStatsByDay rows (pgtype.Date /
// int64) to the route-level []routes.BuildDayCount for the Overview deploys
// sparkline (J-06 D2 / MEET-2026-07-03-003). The handler zero-fills gaps; this
// adapter only converts the sparse tenant-scoped rows.
type BuildStatsStoreAdapter struct {
	Store buildStatsSource
}

func (a *BuildStatsStoreAdapter) BuildStatsByDay(ctx context.Context, tenantID string, since time.Time) ([]buildroutes.BuildDayCount, error) {
	rows, err := a.Store.BuildStatsByDay(ctx, tenantID, since)
	if err != nil {
		return nil, err
	}
	out := make([]buildroutes.BuildDayCount, len(rows))
	for i, r := range rows {
		out[i] = buildroutes.BuildDayCount{Day: r.Day.Time, Count: int(r.Count)}
	}
	return out, nil
}

// The port this adapter satisfies, asserted at compile time.
//
// B-058: the package is called `adapters` and its admission criterion was "it is an adapter" —
// which refuses nothing. Measured on 2026-08-18: of the nine adapters, five NAMED their port in
// prose and only ONE verified it. Prose does not compile; this line does, and it is what lets
// the criterion say no.
var _ sse.BuildStoreReader = (*BuildStoreReaderAdapter)(nil)
