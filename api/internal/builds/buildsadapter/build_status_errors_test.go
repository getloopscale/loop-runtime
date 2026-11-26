// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package buildsadapter

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// A build whose apps cannot be read must not be reported as a build with no
// apps: the status response would claim something the database never said.
func TestGetBuildStatus_SurfacesAnAppsLookupFailure(t *testing.T) {
	lookupErr := errors.New("connection reset")
	a := &BuildOrchestratorAdapter{BuildStore: &fakeBuildStore{
		build:   &db.Build{ID: "b-1", Status: "SUCCEEDED", CreatedAt: time.Now()},
		appsErr: lookupErr,
	}}

	_, err := a.GetBuildStatus(context.Background(), "b-1")
	if !errors.Is(err, lookupErr) {
		t.Fatalf("GetBuildStatus must return the apps lookup error; got %v", err)
	}
}

// apps_spec is JSONB, so it is always valid JSON — but not always the array
// shape the build list expects. That mismatch is a fact about the row, and the
// decoder reports it instead of silently answering "no apps".
func TestDecodeAppsSpecForListing_ReportsAShapeMismatch(t *testing.T) {
	if _, err := decodeAppsSpecForListing(json.RawMessage(`{"name":"api"}`)); err == nil {
		t.Fatal("an object where an array of apps is expected must be reported")
	}
}

func TestDecodeAppsSpecForListing_EmptySpecIsNoApps(t *testing.T) {
	apps, err := decodeAppsSpecForListing(nil)
	if err != nil || apps != nil {
		t.Fatalf("empty spec = (%v, %v), want (nil, nil)", apps, err)
	}
}
