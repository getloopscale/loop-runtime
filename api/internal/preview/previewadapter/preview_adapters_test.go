// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package previewadapter

import (
	"context"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db"
)

// previewBuildStore serves one SUCCEEDED build and its stored apps; every other
// db.BuildStore method is unreachable from the lookup under test.
type previewBuildStore struct {
	db.BuildStore
	apps []db.BuildAppSpec
}

func (s previewBuildStore) ListByProject(context.Context, string, string, int, int) ([]db.Build, error) {
	return []db.Build{{ID: "b-1", Status: "SUCCEEDED"}}, nil
}

func (s previewBuildStore) GetBuildApps(context.Context, string) ([]db.BuildAppSpec, error) {
	return s.apps, nil
}

// A PR preview built from the last successful build must rebuild with that
// build's runtime, path, build mode and start command — without them the
// strategy resolves to unsupported and the preview fails (loopzero#185 class).
func TestPreviewAppsLookup_KeepsTheStoredBuildInstructions(t *testing.T) {
	lookup := &PreviewAppsLookupAdapter{Builds: previewBuildStore{apps: []db.BuildAppSpec{{
		Name: "api", Type: "server", Port: 8080,
		Runtime: "node", Path: "apps/api", Build: "dockerfile", Start: "node server.js",
	}}}}

	apps, err := lookup.LatestSucceededApps(context.Background(), "tn-1", "web")
	if err != nil {
		t.Fatalf("LatestSucceededApps: %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("apps = %d, want 1", len(apps))
	}
	a := apps[0]
	if a.Runtime != "node" || a.Path != "apps/api" || a.Build != "dockerfile" || a.Start != "node server.js" {
		t.Errorf("preview lost the stored build instructions: %+v", a)
	}
}
