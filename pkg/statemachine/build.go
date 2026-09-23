// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

import "time"

// Build states as defined in spec section 5.1.
//
// RELEASING (MEET-2026-03-26-008): intermediate state between RUNNING and SUCCEEDED.
// Means "container image built successfully, release activation in progress."
// SUCCEEDED now means "deploy fully complete (release ACTIVE, app on workload cluster)."
// RELEASING is non-terminal so the reconcile loop picks it up for retry on crash/failure.
const (
	BuildQueued     State = "QUEUED"
	BuildRunning    State = "RUNNING"
	BuildReleasing  State = "RELEASING"
	BuildSucceeded  State = "SUCCEEDED"
	BuildFailed     State = "FAILED"
	BuildCancelling State = "CANCELLING"
	BuildCancelled  State = "CANCELLED"
)

// Build state timeout defaults (spec §5.1).
const (
	BuildQueuedTimeout     = 5 * time.Minute
	BuildRunningTimeout    = 30 * time.Minute
	BuildReleasingTimeout  = 10 * time.Minute
	BuildCancellingTimeout = 60 * time.Second
)

// Build events as defined in spec section 5.1.
const (
	BuildStarted             Event = "BuildStarted"
	BuildSucceededEvent      Event = "BuildSucceeded"        // image built → RELEASING
	BuildReleaseCompleted    Event = "BuildReleaseCompleted" // release ACTIVE → SUCCEEDED
	BuildReleaseFailedEvent  Event = "BuildReleaseFailed"    // release activation permanently failed → FAILED
	BuildReleaseTimeoutEvent Event = "BuildReleaseTimeout"   // RELEASING > 10min → FAILED
	BuildFailedEvent         Event = "BuildFailed"
	BuildCancelRequested     Event = "BuildCancelRequested"
	BuildCancelConfirmed     Event = "BuildCancelConfirmed"
	BuildQueueTimeout        Event = "BuildQueueTimeout"
	BuildTimeout             Event = "BuildTimeout"
	BuildCancelTimeout       Event = "BuildCancelTimeout"
)

// IsBuildTerminal returns true if the given state is a terminal build state
// (SUCCEEDED, FAILED, or CANCELLED). RELEASING is explicitly NOT terminal —
// it must be picked up by the reconcile loop for retry (MEET-2026-03-26-008).
func IsBuildTerminal(state State) bool {
	return buildLifecycle.IsTerminal(state)
}

// buildLifecycle answers the package-level Build helpers from the one
// transition table. A Machine is immutable, so sharing it is safe.
var buildLifecycle = NewBuildMachine()

// NewBuildMachine returns the build state machine per spec section 5.1.
//
// States: QUEUED → RUNNING → RELEASING → SUCCEEDED
//
//	↘ FAILED       ↘ FAILED
//	↘ CANCELLING → CANCELLED
//
// RELEASING (MEET-2026-03-26-008): image built, release activation in progress.
// Terminal: SUCCEEDED, FAILED, CANCELLED
// Timeouts: QUEUED 5min, RUNNING 30min, RELEASING 10min, CANCELLING 60s
func NewBuildMachine() *Machine {
	return NewMachine(MachineConfig{
		Name: "Build",
		Transitions: []transition{
			// QUEUED transitions
			{From: BuildQueued, Event: BuildStarted, To: BuildRunning},
			{From: BuildQueued, Event: BuildCancelRequested, To: BuildCancelled},
			{From: BuildQueued, Event: BuildQueueTimeout, To: BuildFailed},
			{From: BuildQueued, Event: BuildFailedEvent, To: BuildFailed}, // REVIEW-BLOCKER-1: immediate failure on submit error

			// RUNNING transitions
			{From: BuildRunning, Event: BuildSucceededEvent, To: BuildReleasing}, // MEET-008: image built → RELEASING (not SUCCEEDED)
			{From: BuildRunning, Event: BuildFailedEvent, To: BuildFailed},
			{From: BuildRunning, Event: BuildCancelRequested, To: BuildCancelling},
			{From: BuildRunning, Event: BuildTimeout, To: BuildFailed},

			// RELEASING transitions (MEET-2026-03-26-008)
			{From: BuildReleasing, Event: BuildReleaseCompleted, To: BuildSucceeded}, // release ACTIVE → deploy complete
			{From: BuildReleasing, Event: BuildReleaseFailedEvent, To: BuildFailed},  // activation permanently failed
			{From: BuildReleasing, Event: BuildReleaseTimeoutEvent, To: BuildFailed}, // 10min timeout
			{From: BuildReleasing, Event: BuildCancelRequested, To: BuildCancelled},  // user cancel during release

			// CANCELLING transitions
			{From: BuildCancelling, Event: BuildCancelConfirmed, To: BuildCancelled},
			{From: BuildCancelling, Event: BuildCancelTimeout, To: BuildCancelled},
		},
		Terminal: []State{BuildSucceeded, BuildFailed, BuildCancelled},
		Timeouts: []TimeoutConfig{
			{State: BuildQueued, Timeout: BuildQueuedTimeout, Reason: "QUEUE_TIMEOUT"},
			{State: BuildRunning, Timeout: BuildRunningTimeout, Reason: "BUILD_TIMEOUT"},
			{State: BuildReleasing, Timeout: BuildReleasingTimeout, Reason: "RELEASE_TIMEOUT"},
			{State: BuildCancelling, Timeout: BuildCancellingTimeout, Reason: "CANCEL_TIMEOUT"},
		},
	})
}
