// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package events contains the canonical event payload schemas for SSE
// streams. Backend serializers + frontend TS types both reference these
// types via documentation cross-reference.
//
// Plan J-17 v1.1 (2026-06-06). When adding a new event type, ALSO update
// cloud/dashboard/src/lib/api/sse-events.ts so the TS discriminated union
// stays in sync. There is no codegen today; the contract is honor-system.
package events

import "time"

// Event type constants for deployment-related SSE events. Each one
// corresponds to exactly one TypeScript discriminator literal in
// sse-events.ts. NEVER rename or remove without coordinated frontend
// update.
const (
	// Build lifecycle (already emitted by services/build_orchestrator_*).
	EventBuildStarted   = "build.started"
	EventBuildSucceeded = "build.succeeded"
	EventBuildFailed    = "build.failed"

	// Sub-step events (NEW in J-17 T1.1). Emitted by argo_step_watcher
	// when an Argo Workflow Pod node phase transitions.
	EventBuildStepStarted   = "build.step.started"
	EventBuildStepSucceeded = "build.step.succeeded"
	EventBuildStepFailed    = "build.step.failed"

	// Deploy lifecycle (already emitted).
	EventDeployDelivering = "deploy.delivering"
	EventDeployVerifying  = "deploy.verifying"
	EventDeployLive       = "deploy.live"
	EventDeployCompleted  = "deploy.completed"
	EventDeploySyncFailed = "deploy.sync_failed"

	// Rollout lifecycle (already emitted by rollout flow).
	EventRolloutAborted = "rollout.aborted"
)

// CanonicalStepNames is the allowlist of step names that get a dedicated
// Prometheus counter label. Any step not in this list maps to label
// `step="other"` (EC-16 cardinality guard).
//
// Order mirrors the Argo Workflow DAG order in the build pipeline. UI
// groups these into 5 phases (Clone / Install / Build / Sign / Deliver).
var CanonicalStepNames = []string{
	"clone-repo",
	"validate-workspace-node",
	"validate-workspace-user-dockerfile",
	"restore-cache-node",
	"install-deps-node",
	"install-deps-user-dockerfile",
	"security-scan-node",
	"security-scan-user-dockerfile",
	"generate-dockerfile-node",
	"dockerfile-lint-user-dockerfile",
	"build-apps-node",
	"build-apps-user-dockerfile",
	"build-image-api",
	"save-cache-node",
	"sign-image-api",
	"sbom-api",
	"deliver-artifact-web",
}

// IsCanonicalStep reports whether a step name is in the allowlist.
// Used by the metrics layer to clamp cardinality.
func IsCanonicalStep(name string) bool {
	for _, s := range CanonicalStepNames {
		if s == name {
			return true
		}
	}
	return false
}

// BuildStepStartedPayload is the JSON body of a `build.step.started` event.
type BuildStepStartedPayload struct {
	StepName  string    `json:"step_name"`
	StartedAt time.Time `json:"started_at"`
	// PhaseGroup is one of: clone | install | build | sign | deliver | other.
	// Lets the UI group steps without hardcoding the name -> group mapping.
	PhaseGroup string `json:"phase_group"`
}

// BuildStepSucceededPayload is emitted on Pod phase -> Succeeded.
type BuildStepSucceededPayload struct {
	StepName        string    `json:"step_name"`
	StartedAt       time.Time `json:"started_at"`
	EndedAt         time.Time `json:"ended_at"`
	DurationSeconds float64   `json:"duration_seconds"`
	PhaseGroup      string    `json:"phase_group"`
}

// BuildStepFailedPayload includes error classification + actionable
// suggestion for the dashboard (D7).
type BuildStepFailedPayload struct {
	StepName        string    `json:"step_name"`
	StartedAt       time.Time `json:"started_at"`
	EndedAt         time.Time `json:"ended_at"`
	DurationSeconds float64   `json:"duration_seconds"`
	PhaseGroup      string    `json:"phase_group"`
	ErrorClass      string    `json:"error_class"`
	ErrorMessage    string    `json:"error_message"`
	Suggestion      string    `json:"suggestion"`
}

// PhaseGroupForStep returns the UI phase group for a canonical step.
// Unknown steps map to "other" (EC-11 fallback rendering).
func PhaseGroupForStep(stepName string) string {
	switch stepName {
	case "clone-repo",
		"validate-workspace-node",
		"validate-workspace-user-dockerfile":
		return "clone"
	case "restore-cache-node",
		"install-deps-node",
		"install-deps-user-dockerfile",
		"security-scan-node",
		"security-scan-user-dockerfile":
		return "install"
	case "generate-dockerfile-node",
		"dockerfile-lint-user-dockerfile",
		"build-apps-node",
		"build-apps-user-dockerfile",
		"build-image-api",
		"save-cache-node":
		return "build"
	case "sign-image-api",
		"sbom-api":
		return "sign"
	case "deliver-artifact-web":
		return "deliver"
	default:
		return "other"
	}
}
