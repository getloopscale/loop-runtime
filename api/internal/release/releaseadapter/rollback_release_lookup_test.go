// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package releaseadapter

import (
	"context"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// releasesByEnvironment answers ListByEnv from rows keyed by environment, as
// the store does: only the rows of the tenant, project and environment asked.
type releasesByEnvironment struct {
	rows []db.Release
}

func (s releasesByEnvironment) ListByEnv(_ context.Context, tenantID, projectID, env string, _ int) ([]db.Release, error) {
	var out []db.Release
	for _, r := range s.rows {
		if r.TenantID == tenantID && r.ProjectID == projectID && r.Environment == env {
			out = append(out, r)
		}
	}
	return out, nil
}

func releaseRow(id, env, build string, minute int) db.Release {
	return db.Release{
		ID: id, TenantID: "tn-1", ProjectID: "app", Environment: env, BuildID: build,
		CreatedAt: time.Date(2026, 11, 25, 18, minute, 0, 0, time.UTC),
	}
}

// A promote gives the build a newer release in the target environment. A
// rollback of production must still take the release production ran.
func TestRollbackLookup_TakesTheReleaseOfTheEnvironmentRolledBack(t *testing.T) {
	lookup := &RollbackReleaseLookupAdapter{Store: releasesByEnvironment{rows: []db.Release{
		releaseRow("rel-production", "production", "build-1", 17),
		releaseRow("rel-preview", "preview", "build-1", 41),
	}}}

	got, err := lookup.GetReleaseForBuild(context.Background(), "tn-1", "app", "production", "build-1")

	if err != nil {
		t.Fatalf("GetReleaseForBuild: %v", err)
	}
	if got != "rel-production" {
		t.Errorf("release = %q, want rel-production", got)
	}
}

func TestRollbackLookup_TakesTheNewestReleaseOfTheBuildInTheEnvironment(t *testing.T) {
	lookup := &RollbackReleaseLookupAdapter{Store: releasesByEnvironment{rows: []db.Release{
		releaseRow("rel-first", "production", "build-1", 10),
		releaseRow("rel-redeployed", "production", "build-1", 30),
		releaseRow("rel-other-build", "production", "build-2", 50),
	}}}

	got, err := lookup.GetReleaseForBuild(context.Background(), "tn-1", "app", "production", "build-1")

	if err != nil || got != "rel-redeployed" {
		t.Errorf("release = %q, err = %v, want rel-redeployed", got, err)
	}
}

func TestRollbackLookup_FindsNothingForABuildTheEnvironmentNeverRan(t *testing.T) {
	lookup := &RollbackReleaseLookupAdapter{Store: releasesByEnvironment{rows: []db.Release{
		releaseRow("rel-preview", "preview", "build-1", 41),
	}}}

	for name, tenant := range map[string]string{"same tenant": "tn-1", "another tenant": "tn-2"} {
		got, err := lookup.GetReleaseForBuild(context.Background(), tenant, "app", "production", "build-1")
		if err != nil || got != "" {
			t.Errorf("%s: release = %q, err = %v, want none", name, got, err)
		}
	}
}
