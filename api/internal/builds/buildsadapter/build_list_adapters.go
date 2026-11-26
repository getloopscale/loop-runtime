// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package buildsadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/builds/buildshttp"

	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
)

// BuildListStoreAdapter implements routes.BuildListStore using db.BuildStore.
type BuildListStoreAdapter struct {
	Store envBuilds
}

func (a *BuildListStoreAdapter) ListByProjectAndEnv(ctx context.Context, tenantID, projectID, env string, limit, offset int) ([]buildshttp.BuildListItem, int, error) {
	builds, total, err := a.Store.ListByProjectAndEnv(ctx, tenantID, projectID, env, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	result := make([]buildshttp.BuildListItem, len(builds))
	for i, b := range builds {
		apps, aerr := decodeAppsSpecForListing(b.AppsSpec)
		if aerr != nil {
			// apps_spec is a display denormalization; the row still lists, but
			// a spec that does not decode is recorded with the build it names
			// rather than shown as "no apps".
			slog.WarnContext(ctx, "build list: apps_spec does not decode; apps omitted",
				"build_id", b.ID, "tenant_id", tenantID, "error", aerr)
		}
		result[i] = buildshttp.BuildListItem{
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
func decodeAppsSpecForListing(raw json.RawMessage) ([]buildshttp.BuildAppInfo, error) {
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
	out := make([]buildshttp.BuildAppInfo, len(parsed))
	for i, p := range parsed {
		out[i] = buildshttp.BuildAppInfo{Name: p.Name, Type: p.Type}
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

func (a *BuildStatsStoreAdapter) BuildStatsByDay(ctx context.Context, tenantID string, since time.Time) ([]buildshttp.BuildDayCount, error) {
	rows, err := a.Store.BuildStatsByDay(ctx, tenantID, since)
	if err != nil {
		return nil, err
	}
	out := make([]buildshttp.BuildDayCount, len(rows))
	for i, r := range rows {
		out[i] = buildshttp.BuildDayCount{Day: r.Day.Time, Count: int(r.Count)}
	}
	return out, nil
}

// The ports these adapters satisfy, asserted at compile time.
var (
	_ buildshttp.BuildListStore  = (*BuildListStoreAdapter)(nil)
	_ buildshttp.BuildStatsStore = (*BuildStatsStoreAdapter)(nil)
)
