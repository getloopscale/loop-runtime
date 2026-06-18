package enforcement_test

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

	"github.com/getloopscale/loopzero/pkg/enforcement"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// TestMapProjectStatus_ActiveReleaseTrumpsCancelledBuild is the direct
// regression lock for the sponsor-caught bug. Same state as loopzerokit-
// example at the time: an ACTIVE release + a CANCELLED most-recent
// build. The project IS Live regardless of what the latest build did.
func TestMapProjectStatus_ActiveReleaseTrumpsCancelledBuild(t *testing.T) {
	got := enforcement.MapProjectStatus("ACTIVE", statemachine.BuildCancelled)
	if got != enforcement.ProjectStatusLive {
		t.Errorf("got %q; want %q (ACTIVE release should override cancelled build)",
			got, enforcement.ProjectStatusLive)
	}
}

// TestMapProjectStatus_ActiveReleaseTrumpsFailedBuild — defensive
// extension of the regression: even when the latest build genuinely
// failed (not just cancelled), an ACTIVE release still serves
// production. The project is Live until the user rolls back OR a
// successful redeploy supersedes the active release.
func TestMapProjectStatus_ActiveReleaseTrumpsFailedBuild(t *testing.T) {
	got := enforcement.MapProjectStatus("ACTIVE", statemachine.BuildFailed)
	if got != enforcement.ProjectStatusLive {
		t.Errorf("got %q; want %q (ACTIVE release should override failed build)",
			got, enforcement.ProjectStatusLive)
	}
}

// TestMapProjectStatus_RollingBackReleaseIsBuilding — rollback is an
// in-flight transition. The dashboard treats it as "Building" (the
// existing in-flight bucket) so the StatusDot animates and the user
// knows the project is mid-flight, not Live.
func TestMapProjectStatus_RollingBackReleaseIsBuilding(t *testing.T) {
	got := enforcement.MapProjectStatus("ROLLING_BACK", statemachine.BuildSucceeded)
	if got != enforcement.ProjectStatusBuilding {
		t.Errorf("got %q; want %q",
			got, enforcement.ProjectStatusBuilding)
	}
}

// TestMapProjectStatus_FailedReleaseIsFailed — the release-state SoT
// genuinely reports Failed (release stuck in FAILED or ROLLBACK_FAILED).
// The project IS Failed; do NOT fall back to the latest build.
func TestMapProjectStatus_FailedReleaseIsFailed(t *testing.T) {
	for _, releaseState := range []string{"FAILED", "ROLLBACK_FAILED"} {
		got := enforcement.MapProjectStatus(releaseState, statemachine.BuildSucceeded)
		if got != enforcement.ProjectStatusFailed {
			t.Errorf("%q: got %q; want %q",
				releaseState, got, enforcement.ProjectStatusFailed)
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
		want       enforcement.ProjectStatus
	}{
		{statemachine.BuildQueued, enforcement.ProjectStatusQueued},
		{statemachine.BuildRunning, enforcement.ProjectStatusBuilding},
		{statemachine.BuildSucceeded, enforcement.ProjectStatusLive},
		{statemachine.BuildFailed, enforcement.ProjectStatusFailed},
	}
	for _, c := range cases {
		got := enforcement.MapProjectStatus("", c.buildState)
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
	got := enforcement.MapProjectStatus("", statemachine.BuildCancelled)
	if got != enforcement.ProjectStatusIdle {
		t.Errorf("got %q; want %q (cancelled != failed)",
			got, enforcement.ProjectStatusIdle)
	}
	// Direct mapper call too — the underlying MapBuildStatusToProjectStatus
	// must NOT collapse cancelled into failed anymore.
	if got := enforcement.MapBuildStatusToProjectStatus(statemachine.BuildCancelled); got != enforcement.ProjectStatusIdle {
		t.Errorf("MapBuildStatusToProjectStatus(BuildCancelled) = %q; want %q",
			got, enforcement.ProjectStatusIdle)
	}
}

// TestMapProjectStatus_EmptyEverythingIsIdle — cold project with no
// release and no build at all. Defensive Idle (matches the
// MapBuildStatusToProjectStatus empty-state behavior).
func TestMapProjectStatus_EmptyEverythingIsIdle(t *testing.T) {
	got := enforcement.MapProjectStatus("", "")
	if got != enforcement.ProjectStatusIdle {
		t.Errorf("got %q; want %q", got, enforcement.ProjectStatusIdle)
	}
}

// TestMapProjectStatus_SupersededReleaseFallsBackToBuild — terminal
// non-active release states (SUPERSEDED, ROLLED_BACK, CANCELLED) are
// historical, not the current truth. Fall back to the latest build.
// A project whose last release was SUPERSEDED by a now-failed build
// IS Failed (the most recent attempt failed and there's nothing newer
// serving traffic).
func TestMapProjectStatus_SupersededReleaseFallsBackToBuild(t *testing.T) {
	for _, terminalRelease := range []string{"SUPERSEDED", "ROLLED_BACK", "CANCELLED"} {
		got := enforcement.MapProjectStatus(terminalRelease, statemachine.BuildFailed)
		if got != enforcement.ProjectStatusFailed {
			t.Errorf("%q + BuildFailed: got %q; want %q (terminal release should fall back)",
				terminalRelease, got, enforcement.ProjectStatusFailed)
		}
	}
}
