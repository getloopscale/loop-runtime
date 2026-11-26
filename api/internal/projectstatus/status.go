// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package projectstatus maps a project's latest build and release state to the
// deploy status the /projects list shows (the dashboard's StatusDot). It is
// presentation of engine state, so it lives beside the API that serves it
// rather than in the shared plan-limit policy package.
//
// Per plan 2026-06-08-dashboard-screens-review-fixes T2.1 + ADR D2 +
// EC-1 absorption. Exhaustive 7-state mapping locks the "code is
// authoritative" contract from CLAUDE.md Rule 2: dashboard consumes
// the mapping but does NOT re-derive it client-side.
package projectstatus

import (
	"github.com/getloopscale/contracts/plan"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// ProjectStatus is the dashboard-facing deploy-status enum. M1.2/M1.3: the
// enum moved to pkg/plan (value vocabulary); the MAPPING below stays
// engine-side because it imports pkg/statemachine, which must NEVER reach the
// contracts surface (plan m1-decouple-in-place D3). The aliases are retained as
// package-internal shorthand (external consumers import plan.ProjectStatus*
// directly); not a re-export path (v1.2 amendment).
type ProjectStatus = plan.ProjectStatus

const (
	ProjectStatusLive     = plan.ProjectStatusLive
	ProjectStatusBuilding = plan.ProjectStatusBuilding
	ProjectStatusFailed   = plan.ProjectStatusFailed
	ProjectStatusIdle     = plan.ProjectStatusIdle
	ProjectStatusQueued   = plan.ProjectStatusQueued
	// Red Team loopruntime-cloud#41 (2026-06-27) — preview-only project status.
	// Declared as a local typed string to avoid pinning the engine on a
	// not-yet-published loopruntime-contracts v0.2 (the canonical constant lives
	// at loopruntime-contracts/plan.ProjectStatusPreview — see follow-up
	// version-pin cutover runbook). Wire-format identical: "preview".
	ProjectStatusPreview plan.ProjectStatus = "preview"
)

// MapBuildStatusToProjectStatus is the exhaustive 7-state mapping.
// Per EC-1 absorption: ALL pkg/statemachine.BuildState constants are
// listed below. If a new BuildState is added to pkg/statemachine, the
// corresponding case MUST be added here AND status_mapper_test.go's
// length guard updated — otherwise the new state falls through to
// ProjectStatusIdle defensively (no panic in production path).
//
// 2026-06-18 semantic correction (sponsor LIVE-caught): BuildCancelled
// now maps to ProjectStatusIdle, NOT ProjectStatusFailed. Cancellation
// is intentional (user pressed Cancel); failure is unexpected. Sharing
// a UI bucket produced visually-broken /projects rows for cancelled-
// build projects that still had an ACTIVE release serving production.
// Callers that need the canonical release-aware aggregate status (the
// "is this project Live?" question) should use MapProjectStatus instead.
func MapBuildStatusToProjectStatus(state statemachine.State) ProjectStatus {
	switch state {
	case statemachine.BuildSucceeded:
		return ProjectStatusLive
	case statemachine.BuildFailed:
		return ProjectStatusFailed
	case statemachine.BuildCancelled:
		return ProjectStatusIdle
	case statemachine.BuildRunning,
		statemachine.BuildReleasing,
		statemachine.BuildCancelling:
		return ProjectStatusBuilding
	case statemachine.BuildQueued:
		return ProjectStatusQueued
	}
	return ProjectStatusIdle
}

// MapProjectStatus is the canonical release-aware aggregator answering
// "what is this project's deploy status?" from the user's mental model:
// "is the project currently serving traffic?". The release-state SoT
// trumps the latest build state because a build is just an attempt —
// the release is the canonical observation of what landed.
//
// activeRelease is the state of the release the projects summary query
// (ListProjectsWithSummaryByTenant) selected for the project; the zero State
// means no such release. Every Release state is decided here — first match
// wins:
//
//   - ACTIVE → Live. The project has a release serving production; the
//     latest build is irrelevant (it may have been cancelled or failed).
//
//   - READY, PENDING, ROLLING_BACK → Building. A release is in flight, so
//     the StatusDot animates instead of reporting a build result.
//
//   - GIT_MIRROR_FAILED, SYNC_FAILED, DRIFT_ALERT, ROLLBACK_FAILED → Failed.
//     The release itself failed to land (or to roll back); a succeeded build
//     behind it must not read as Live.
//
//   - SUPERSEDED, ROLLED_BACK, CANCELLED, ARCHIVED, none → fall back to
//     MapBuildStatusToProjectStatus on the latest build: a historical
//     release is not the current truth, the latest build attempt is.
//
// latestBuildEnv is the latest build's `environment` column. Red Team
// loopruntime-cloud#41 absorption (2026-06-27): when no release decides the status
// and the latest build SUCCEEDED in a preview-only environment, return
// ProjectStatusPreview — the project is preview-only, so the dashboard
// renders a "Preview" badge instead of "Live" with a confusing
// "URL PENDING" hint.
func MapProjectStatus(activeRelease, latestBuildState statemachine.State, latestBuildEnv string) ProjectStatus {
	switch activeRelease {
	case statemachine.ReleaseActive:
		return ProjectStatusLive
	case statemachine.ReleaseReady,
		statemachine.ReleasePending,
		statemachine.ReleaseRollingBack:
		return ProjectStatusBuilding
	case statemachine.ReleaseGitMirrorFailed,
		statemachine.ReleaseSyncFailed,
		statemachine.ReleaseDriftAlert,
		statemachine.ReleaseRollbackFailed:
		return ProjectStatusFailed
	}
	if latestBuildState == statemachine.BuildSucceeded && latestBuildEnv == "preview" {
		return ProjectStatusPreview
	}
	return MapBuildStatusToProjectStatus(latestBuildState)
}
