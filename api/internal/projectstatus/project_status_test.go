// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package projectstatus_test

// Sponsor LIVE-caught 2026-06-18: /projects listing showed "Failed" for
// loopzerokit-example while the project actually had an ACTIVE release
// serving production (the user had cancelled an in-flight build, but
// the prior release was still live). The pre-existing aggregate logic
// only consulted the latest build status, ignoring the canonical
// release-state SoT.
//
// The release-aware mapper resolves "what is this project's status?"
// from the canonical mental model:
//   - "Has the project shipped something that is currently serving
//     traffic?" → release state IS the truth, NOT the latest build.
//   - When a build is cancelled (user pressed Cancel) but a prior
//     release is still ACTIVE, the project is Live, not Failed.
//   - When no release has ever been created (cold project, only
//     queued/running/failed builds), fall back to the latest build
//     state via MapBuildStatusToProjectStatus.
//
// Tests below pin the canonical decision tree so a future refactor
// cannot regress to "use last build status only".

import (
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/projectstatus"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// TestMapProjectStatus_ActiveReleaseTrumpsCancelledBuild is the direct
// regression lock for the sponsor-caught bug. Same state as loopzerokit-
// example at the time: an ACTIVE release + a CANCELLED most-recent
// build. The project IS Live regardless of what the latest build did.
func TestMapProjectStatus_ActiveReleaseTrumpsCancelledBuild(t *testing.T) {
	got := projectstatus.MapProjectStatus(statemachine.ReleaseActive, statemachine.BuildCancelled, "production")
	if got != projectstatus.ProjectStatusLive {
		t.Errorf("got %q; want %q (ACTIVE release should override cancelled build)",
			got, projectstatus.ProjectStatusLive)
	}
}

// TestMapProjectStatus_ActiveReleaseTrumpsFailedBuild — defensive
// extension of the regression: even when the latest build genuinely
// failed (not just cancelled), an ACTIVE release still serves
// production. The project is Live until the user rolls back OR a
// successful redeploy supersedes the active release.
func TestMapProjectStatus_ActiveReleaseTrumpsFailedBuild(t *testing.T) {
	got := projectstatus.MapProjectStatus(statemachine.ReleaseActive, statemachine.BuildFailed, "production")
	if got != projectstatus.ProjectStatusLive {
		t.Errorf("got %q; want %q (ACTIVE release should override failed build)",
			got, projectstatus.ProjectStatusLive)
	}
}

// TestMapProjectStatus_RollingBackReleaseIsBuilding — rollback is an
// in-flight transition. The dashboard treats it as "Building" (the
// existing in-flight bucket) so the StatusDot animates and the user
// knows the project is mid-flight, not Live.
func TestMapProjectStatus_RollingBackReleaseIsBuilding(t *testing.T) {
	got := projectstatus.MapProjectStatus(statemachine.ReleaseRollingBack, statemachine.BuildSucceeded, "production")
	if got != projectstatus.ProjectStatusBuilding {
		t.Errorf("got %q; want %q",
			got, projectstatus.ProjectStatusBuilding)
	}
}

// TestMapProjectStatus_FailedReleaseIsFailed — the release-state SoT
// genuinely reports Failed (release stuck in SYNC_FAILED or ROLLBACK_FAILED).
// The project IS Failed; do NOT fall back to the latest build.
func TestMapProjectStatus_FailedReleaseIsFailed(t *testing.T) {
	for _, releaseState := range []statemachine.State{statemachine.ReleaseSyncFailed, statemachine.ReleaseRollbackFailed} {
		got := projectstatus.MapProjectStatus(releaseState, statemachine.BuildSucceeded, "production")
		if got != projectstatus.ProjectStatusFailed {
			t.Errorf("%q: got %q; want %q",
				releaseState, got, projectstatus.ProjectStatusFailed)
		}
	}
}

// TestMapProjectStatus_NoReleaseFallsBackToLatestBuild — when the
// project has NEVER had a release (cold project, only running/failed
// builds), the build state is the only signal available. Fall back to
// the existing MapBuildStatusToProjectStatus contract.
func TestMapProjectStatus_NoReleaseFallsBackToLatestBuild(t *testing.T) {
	cases := []struct {
		buildState statemachine.State
		want       projectstatus.ProjectStatus
	}{
		{statemachine.BuildQueued, projectstatus.ProjectStatusQueued},
		{statemachine.BuildRunning, projectstatus.ProjectStatusBuilding},
		{statemachine.BuildSucceeded, projectstatus.ProjectStatusLive},
		{statemachine.BuildFailed, projectstatus.ProjectStatusFailed},
	}
	for _, c := range cases {
		got := projectstatus.MapProjectStatus("", c.buildState, "production")
		if got != c.want {
			t.Errorf("empty release + build=%q: got %q; want %q",
				c.buildState, got, c.want)
		}
	}
}

// TestMapProjectStatus_CancelledBuildIsIdleNotFailed locks the
// semantic correction. The pre-fix mapper treated cancelled and
// failed identically — but cancellation is intentional (user pressed
// Cancel), failure is unexpected. They cannot share a UI bucket.
// Cancelled now lands in Idle so the project doesn't visually scream
// "broken" for an action the user explicitly took.
func TestMapProjectStatus_CancelledBuildIsIdleNotFailed(t *testing.T) {
	// No release yet → fall through to latest build → CANCELLED → Idle.
	got := projectstatus.MapProjectStatus("", statemachine.BuildCancelled, "production")
	if got != projectstatus.ProjectStatusIdle {
		t.Errorf("got %q; want %q (cancelled != failed)",
			got, projectstatus.ProjectStatusIdle)
	}
	// Direct mapper call too — the underlying MapBuildStatusToProjectStatus
	// must NOT collapse cancelled into failed anymore.
	if got := projectstatus.MapBuildStatusToProjectStatus(statemachine.BuildCancelled); got != projectstatus.ProjectStatusIdle {
		t.Errorf("MapBuildStatusToProjectStatus(BuildCancelled) = %q; want %q",
			got, projectstatus.ProjectStatusIdle)
	}
}

// TestMapProjectStatus_EmptyEverythingIsIdle — cold project with no
// release and no build at all. Defensive Idle (matches the
// MapBuildStatusToProjectStatus empty-state behavior).
func TestMapProjectStatus_EmptyEverythingIsIdle(t *testing.T) {
	got := projectstatus.MapProjectStatus("", "", "")
	if got != projectstatus.ProjectStatusIdle {
		t.Errorf("got %q; want %q", got, projectstatus.ProjectStatusIdle)
	}
}

// TestMapProjectStatus_SupersededReleaseFallsBackToBuild — terminal
// non-active release states (SUPERSEDED, ROLLED_BACK, CANCELLED) are
// historical, not the current truth. Fall back to the latest build.
// A project whose last release was SUPERSEDED by a now-failed build
// IS Failed (the most recent attempt failed and there's nothing newer
// serving traffic).
func TestMapProjectStatus_SupersededReleaseFallsBackToBuild(t *testing.T) {
	for _, terminalRelease := range []statemachine.State{statemachine.ReleaseSuperseded, statemachine.ReleaseRolledBack, statemachine.ReleaseCancelled} {
		got := projectstatus.MapProjectStatus(terminalRelease, statemachine.BuildFailed, "production")
		if got != projectstatus.ProjectStatusFailed {
			t.Errorf("%q + BuildFailed: got %q; want %q (terminal release should fall back)",
				terminalRelease, got, projectstatus.ProjectStatusFailed)
		}
	}
}

// TestMapProjectStatus_RedTeam41_PreviewOnlyProject — Red Team
// loop-console#41 absorption (2026-06-27). A project with 12 successful
// builds, all environment=preview, AND zero active production releases
// previously rendered "Live" badge with "URL PENDING" hint across 4
// surfaces. With the latestBuildEnv signal, the canonical aggregator
// returns ProjectStatusPreview so the dashboard renders a distinct
// "Preview" badge (no contradiction between badge + URL state).
func TestMapProjectStatus_RedTeam41_PreviewOnlyProject(t *testing.T) {
	got := projectstatus.MapProjectStatus("", statemachine.BuildSucceeded, "preview")
	if got != projectstatus.ProjectStatusPreview {
		t.Errorf("Red Team #41: BuildSucceeded + preview env + no release MUST map to Preview, got %q", got)
	}
}

// TestMapProjectStatus_RedTeam41_ProductionBuildSucceededIsLive — control:
// a SUCCEEDED build in production env STILL returns Live when there's no
// release (the historical behavior). Only the preview-env case diverges.
func TestMapProjectStatus_RedTeam41_ProductionBuildSucceededIsLive(t *testing.T) {
	got := projectstatus.MapProjectStatus("", statemachine.BuildSucceeded, "production")
	if got != projectstatus.ProjectStatusLive {
		t.Errorf("Red Team #41 control: production env succeeded build STAYS Live, got %q", got)
	}
}

// TestMapProjectStatus_RedTeam41_ActiveReleaseStillWinsOverPreviewBuild —
// release-state SoT remains authoritative. Even with a preview-env latest
// build, an ACTIVE production release maps to Live (the project IS
// serving traffic from the prior production release).
func TestMapProjectStatus_RedTeam41_ActiveReleaseStillWinsOverPreviewBuild(t *testing.T) {
	got := projectstatus.MapProjectStatus(statemachine.ReleaseActive, statemachine.BuildSucceeded, "preview")
	if got != projectstatus.ProjectStatusLive {
		t.Errorf("Red Team #41: ACTIVE release MUST trump preview build env, got %q", got)
	}
}

// TestMapProjectStatus_DecidesEveryReleaseState walks every state of the
// Release state machine with a SUCCEEDED latest build, so a release state
// that reports a failure can never be hidden behind the build's "Live". A new
// Release state must be added here with its intended bucket.
func TestMapProjectStatus_DecidesEveryReleaseState(t *testing.T) {
	const fallsBackToBuild = projectstatus.ProjectStatusLive // BuildSucceeded in production
	cases := map[statemachine.State]projectstatus.ProjectStatus{
		statemachine.ReleaseActive:          projectstatus.ProjectStatusLive,
		statemachine.ReleaseReady:           projectstatus.ProjectStatusBuilding,
		statemachine.ReleasePending:         projectstatus.ProjectStatusBuilding,
		statemachine.ReleaseRollingBack:     projectstatus.ProjectStatusBuilding,
		statemachine.ReleaseGitMirrorFailed: projectstatus.ProjectStatusFailed,
		statemachine.ReleaseSyncFailed:      projectstatus.ProjectStatusFailed,
		statemachine.ReleaseDriftAlert:      projectstatus.ProjectStatusFailed,
		statemachine.ReleaseRollbackFailed:  projectstatus.ProjectStatusFailed,
		statemachine.ReleaseSuperseded:      fallsBackToBuild,
		statemachine.ReleaseRolledBack:      fallsBackToBuild,
		statemachine.ReleaseCancelled:       fallsBackToBuild,
		statemachine.ReleaseArchived:        fallsBackToBuild,
	}
	machine := statemachine.NewReleaseMachine()
	for state, want := range cases {
		if !machine.IsKnownState(state) {
			t.Fatalf("%q is not a Release state", state)
		}
		if got := projectstatus.MapProjectStatus(state, statemachine.BuildSucceeded, "production"); got != want {
			t.Errorf("release %q: got %q, want %q", state, got, want)
		}
	}
}
