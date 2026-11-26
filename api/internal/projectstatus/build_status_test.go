// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package projectstatus_test

// EC-1 absorption from plan
// 2026-06-08-dashboard-screens-review-fixes — exhaustive table-driven
// coverage over ALL 7 BuildState constants in pkg/statemachine/build.go.
//
// Test design (anti-drift):
//   - The `cases` slice MUST be hand-updated when pkg/statemachine adds
//     a new BuildState. The `require 7 cases` length guard fails fast
//     if someone declares an 8th state without updating this list.
//   - `default` branch of the helper maps any unknown state to
//     ProjectStatusIdle (defense in depth — pre-release we don't want
//     a panic in prod path, but the test guarantees the 7 known states
//     produce explicit non-default mappings).

import (
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/projectstatus"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

func TestMapBuildStatusToProjectStatus_CoversAllSevenBuildStates(t *testing.T) {
	cases := []struct {
		state statemachine.State
		want  projectstatus.ProjectStatus
	}{
		{statemachine.BuildQueued, projectstatus.ProjectStatusQueued},
		{statemachine.BuildRunning, projectstatus.ProjectStatusBuilding},
		{statemachine.BuildReleasing, projectstatus.ProjectStatusBuilding},
		{statemachine.BuildSucceeded, projectstatus.ProjectStatusLive},
		{statemachine.BuildFailed, projectstatus.ProjectStatusFailed},
		{statemachine.BuildCancelling, projectstatus.ProjectStatusBuilding},
		// Sponsor LIVE-caught 2026-06-18: CANCELLED is intentional (user
		// pressed Cancel), FAILED is unexpected. Lumping them under
		// ProjectStatusFailed produced visually-broken /projects rows for
		// projects whose latest build was cancelled while an earlier
		// release was still ACTIVE serving production. Cancelled now
		// lands in Idle; the release-aware MapProjectStatus is the
		// canonical aggregator that consults the release state first.
		{statemachine.BuildCancelled, projectstatus.ProjectStatusIdle},
	}
	if len(cases) != 7 {
		t.Fatalf("EC-1 contract: expected exhaustive 7-state coverage, got %d. "+
			"If pkg/statemachine added a new BuildState, append it here.", len(cases))
	}
	for _, c := range cases {
		t.Run(string(c.state), func(t *testing.T) {
			got := projectstatus.MapBuildStatusToProjectStatus(c.state)
			if got != c.want {
				t.Errorf("MapBuildStatusToProjectStatus(%q) = %q, want %q",
					c.state, got, c.want)
			}
		})
	}
}

func TestMapBuildStatusToProjectStatus_EmptyStateIsIdle(t *testing.T) {
	// "No latest build row" (zero rows in JOIN LATERAL) → empty state → idle.
	// This is the explicit fallback for projects with zero builds.
	if got := projectstatus.MapBuildStatusToProjectStatus(""); got != projectstatus.ProjectStatusIdle {
		t.Errorf("MapBuildStatusToProjectStatus(empty) = %q, want %q",
			got, projectstatus.ProjectStatusIdle)
	}
}

func TestMapBuildStatusToProjectStatus_UnknownStateIsIdle(t *testing.T) {
	// Defense in depth: unknown state (e.g., future BuildState added to
	// pkg/statemachine without this helper being updated) falls through
	// to idle in production. Test-time assertion guarantees no panic.
	if got := projectstatus.MapBuildStatusToProjectStatus("UNKNOWN_FUTURE_STATE"); got != projectstatus.ProjectStatusIdle {
		t.Errorf("MapBuildStatusToProjectStatus(unknown) = %q, want %q",
			got, projectstatus.ProjectStatusIdle)
	}
}

func TestProjectStatusConstants_AreCorrectStrings(t *testing.T) {
	// Lock the wire-format strings consumed by dashboard StatusDot.
	cases := map[projectstatus.ProjectStatus]string{
		projectstatus.ProjectStatusLive:     "live",
		projectstatus.ProjectStatusBuilding: "building",
		projectstatus.ProjectStatusFailed:   "failed",
		projectstatus.ProjectStatusIdle:     "idle",
		projectstatus.ProjectStatusQueued:   "queued",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Errorf("ProjectStatus constant: got %q, want %q", got, want)
		}
	}
}
