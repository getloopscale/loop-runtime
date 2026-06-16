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
	"github.com/loopzero/loopzero/pkg/statemachine"
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
)

// MapBuildStatusToProjectStatus is the exhaustive 7-state mapping.
// Per EC-1 absorption: ALL pkg/statemachine.BuildState constants are
// listed below. If a new BuildState is added to pkg/statemachine, the
// corresponding case MUST be added here AND status_mapper_test.go's
// length guard updated — otherwise the new state falls through to
// ProjectStatusIdle defensively (no panic in production path).
func MapBuildStatusToProjectStatus(state statemachine.State) ProjectStatus {
	switch state {
	case statemachine.BuildSucceeded:
		return ProjectStatusLive
	case statemachine.BuildFailed, statemachine.BuildCancelled:
		return ProjectStatusFailed
	case statemachine.BuildRunning,
		statemachine.BuildReleasing,
		statemachine.BuildCancelling:
		return ProjectStatusBuilding
	case statemachine.BuildQueued:
		return ProjectStatusQueued
	}
	return ProjectStatusIdle
}
