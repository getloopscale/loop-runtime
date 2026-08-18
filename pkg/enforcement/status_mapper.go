package enforcement

// Helper that maps a build-state string (the status persisted on the
// builds table — `pkg/statemachine.BuildState` constants) to the
// dashboard-facing ProjectStatus enum surfaced on /projects's StatusDot.
//
// Per plan 2026-06-08-dashboard-screens-review-fixes T2.1 + ADR D2 +
// EC-1 absorption. Exhaustive 7-state mapping locks the "code is
// authoritative" contract from CLAUDE.md Rule 2: dashboard consumes
// the mapping but does NOT re-derive it client-side.

import (
	"github.com/getloopscale/loopzero-contracts/plan"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// ProjectStatus is the dashboard-facing deploy-status enum. M1.2/M1.3: the
// enum moved to pkg/plan (value vocabulary); the MAPPING below stays
// engine-side because it imports pkg/statemachine, which must NEVER reach the
// contracts surface (plan m1-decouple-in-place D3). The aliases are retained as
// enforcement-internal shorthand (external consumers import plan.ProjectStatus*
// directly); not a re-export path (v1.2 amendment).
type ProjectStatus = plan.ProjectStatus

const (
	ProjectStatusLive     = plan.ProjectStatusLive
	ProjectStatusBuilding = plan.ProjectStatusBuilding
	ProjectStatusFailed   = plan.ProjectStatusFailed
	ProjectStatusIdle     = plan.ProjectStatusIdle
	ProjectStatusQueued   = plan.ProjectStatusQueued
	// Red Team loopzero-cloud#41 (2026-06-27) — preview-only project status.
	// Declared as a local typed string to avoid pinning the engine on a
	// not-yet-published loopzero-contracts v0.2 (the canonical constant lives
	// at loopzero-contracts/plan.ProjectStatusPreview — see follow-up
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
// Decision tree (in order — first match wins):
//
//   - activeReleaseStatus == "ACTIVE" → Live
//     The project has a release serving production. The latest build
//     is irrelevant — it may have been cancelled, failed, or be still
//     running; users care that traffic is being served.
//
//   - activeReleaseStatus == "ROLLING_BACK" → Building (in-flight)
//     A rollback is mid-flight. We surface this in the existing
//     "Building" bucket so the StatusDot animates and the user knows
//     the project is mid-flight, not idle.
//
//   - activeReleaseStatus ∈ {"FAILED", "ROLLBACK_FAILED"} → Failed
//     The release-state SoT genuinely reports failure (e.g., manifest
//     reconcile permanently failed, rollback collapsed). Do NOT fall
//     back to the latest build — the release is the authoritative
//     "what landed" answer.
//
//   - otherwise → fall back to MapBuildStatusToProjectStatus on the
//     latest build state. This covers:
//
//   - Cold projects (no release ever) — only build status to read.
//
//   - Terminal non-active releases (SUPERSEDED, ROLLED_BACK,
//     CANCELLED) — the historical release is not the current truth;
//     the latest build attempt is.
//
// activeReleaseStatus is the string from the releases table (uppercased
// status enum). Empty string means "no eligible release row" — the
// SQL query returns this when the project has no release at all OR
// when the only releases are in a terminal state.
//
// latestBuildEnv is the latest build's `environment` column ("preview",
// "production", "staging", ...). Red Team loopzero-cloud#41 absorption
// (2026-06-27): when there is no active release AND the latest build
// SUCCEEDED in a preview-only environment, return ProjectStatusPreview
// — the project is preview-only (no production release ever shipped),
// so the dashboard renders a "Preview" badge instead of "Live" with a
// confusing "URL PENDING" hint. Empty latestBuildEnv = unknown
// (defensive — fall back to the legacy build-state mapping).
func MapProjectStatus(activeReleaseStatus string, latestBuildState statemachine.State, latestBuildEnv string) ProjectStatus {
	switch activeReleaseStatus {
	case "ACTIVE":
		return ProjectStatusLive
	case "ROLLING_BACK":
		return ProjectStatusBuilding
	case "FAILED", "ROLLBACK_FAILED":
		return ProjectStatusFailed
	}
	// Red Team #41: BuildSucceeded + preview env + no active release =
	// preview-only project. Surface as "Preview" instead of "Live".
	if latestBuildState == statemachine.BuildSucceeded && latestBuildEnv == "preview" {
		return ProjectStatusPreview
	}
	return MapBuildStatusToProjectStatus(latestBuildState)
}
