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

import (
	"strings"
	"time"
)

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

// stepFamily is one kind of build pipeline step. A step belongs to a family
// when its name IS the family or is the family followed by "-" and a suffix:
// a language for the per-language steps of a multi-language project
// ("install-deps-python"), an app name for the per-app delivery steps
// ("build-image-checkout").
type stepFamily struct {
	name  string
	phase string
}

// stepFamilies is the single source of truth for step -> UI phase grouping,
// shared by the SSE timeline, the step metric and the build log labels.
// Order mirrors the build pipeline DAG. The UI groups these into five phases
// (Clone / Install / Build / Sign / Deliver); anything else is "other".
var stepFamilies = []stepFamily{
	{"clone-repo", "clone"},
	{"validate-workspace", "clone"},
	{"restore-cache", "install"},
	{"install-deps", "install"},
	{"security-scan", "install"},
	{"generate-dockerfile", "build"},
	{"build-apps", "build"},
	{"build-image", "build"},
	{"save-cache", "build"},
	{"sign-image", "sign"},
	{"sbom", "sign"},
	{"attest-image", "sign"},
	{"deliver-artifact", "deliver"},
}

func familyOf(stepName string) (stepFamily, bool) {
	for _, f := range stepFamilies {
		if stepName == f.name || strings.HasPrefix(stepName, f.name+"-") {
			return f, true
		}
	}
	return stepFamily{}, false
}

// StepMetricLabel returns the step's family ("build-image" for
// "build-image-checkout"), or "other". Families are a fixed set, so the label
// stays bounded however many apps and languages projects use (EC-16).
func StepMetricLabel(stepName string) string {
	if f, ok := familyOf(stepName); ok {
		return f.name
	}
	return "other"
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

// PhaseGroupForStep returns the UI phase group (clone | install | build |
// sign | deliver) of a build pipeline step, or "other" for a step outside
// every family (EC-11 fallback rendering).
func PhaseGroupForStep(stepName string) string {
	if f, ok := familyOf(stepName); ok {
		return f.phase
	}
	return "other"
}
