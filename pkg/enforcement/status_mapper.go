package enforcement

// Helper that maps a build-state string (the status persisted on the
// builds table — `pkg/statemachine.BuildState` constants) to the
// dashboard-facing ProjectStatus enum surfaced on /projects's StatusDot.
//
// Per plan 2026-06-08-dashboard-screens-review-fixes T2.1 + ADR D2 +
// EC-1 absorption. Exhaustive 7-state mapping locks the "code is
// authoritative" contract from CLAUDE.md Rule 2: dashboard consumes
// the mapping but does NOT re-derive it client-side.

import "github.com/loopzero/loopzero/pkg/statemachine"

// ProjectStatus is the dashboard-facing deploy-status enum (5 values).
// Wire-format strings are CONTRACT — pinned by status_mapper_test.go.
type ProjectStatus string

const (
	// ProjectStatusLive — latest build SUCCEEDED; deployment serving.
	ProjectStatusLive ProjectStatus = "live"

	// ProjectStatusBuilding — latest build is in flight
	// (RUNNING / RELEASING / CANCELLING). Dashboard renders a spinner.
	ProjectStatusBuilding ProjectStatus = "building"

	// ProjectStatusFailed — latest build FAILED or was CANCELLED.
	// Cancelled is treated as failure (operator intent was abort).
	ProjectStatusFailed ProjectStatus = "failed"

	// ProjectStatusIdle — project has zero builds OR latest build state
	// is unknown (defense-in-depth fallback).
	ProjectStatusIdle ProjectStatus = "idle"

	// ProjectStatusQueued — latest build is QUEUED (not yet started).
	ProjectStatusQueued ProjectStatus = "queued"
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
