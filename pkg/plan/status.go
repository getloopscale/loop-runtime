// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package plan

// ProjectStatus is the dashboard-facing deploy-status enum (5 values).
// Wire-format strings are CONTRACT — pinned by plan_test.go and the engine's
// status_mapper_test.go. The MAPPING from a build state machine to this enum
// is engine logic and stays in pkg/enforcement (MapBuildStatusToProjectStatus)
// so pkg/plan never imports pkg/statemachine (plan m1-decouple-in-place D3).
type ProjectStatus string

const (
	// ProjectStatusLive — latest build SUCCEEDED; deployment serving.
	ProjectStatusLive ProjectStatus = "live"

	// ProjectStatusBuilding — latest build is in flight
	// (RUNNING / RELEASING / CANCELLING). Dashboard renders a spinner.
	ProjectStatusBuilding ProjectStatus = "building"

	// ProjectStatusFailed — latest build FAILED or was CANCELLED.
	ProjectStatusFailed ProjectStatus = "failed"

	// ProjectStatusIdle — project has zero builds OR latest build state
	// is unknown (defense-in-depth fallback).
	ProjectStatusIdle ProjectStatus = "idle"

	// ProjectStatusQueued — latest build is QUEUED (not yet started).
	ProjectStatusQueued ProjectStatus = "queued"
)
