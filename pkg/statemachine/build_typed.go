// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

// Phase 2 of ADR `2026-05-15-T6-1-typed-state-machines.md`
// (sponsor-ratified 2026-05-16, Phase 1 shipped iter 31):
//
// Typed wrapper for the Build lifecycle state machine. Coexists with
// the untyped `NewBuildMachine() *Machine` — callers migrate
// incrementally per Ralph iter without breaking the wire format.
//
// Design rationale (same as ExposedRouteMachine in exposedroute_typed.go):
//   - distinct types `BuildPhase string` + `BuildPhaseEvent string`
//   - byte-for-byte string equality with untyped State/Event constants
//   - typed Transition / TryTransition / IsTerminal / etc. methods
//
// NOTE: type names use `BuildPhase` instead of `BuildState` to avoid
// collision with the existing `BuildState` Go identifier that some
// callers already use as a local variable name. The Phase suffix
// matches the `loopzerov1.BuildPhase` CR field naming convention.

// BuildPhase is the typed state for the Build lifecycle.
// Underlying string values are identical to the untyped State
// constants (BuildQueued/Running/Releasing/Succeeded/Failed/
// Cancelling/Cancelled) — DB persistence + CR status field +
// SSE events round-trip without translation.
type BuildPhase string

// BuildPhaseEvent is the typed event for the Build lifecycle.
type BuildPhaseEvent string

// Typed state constants (mirror the untyped BuildX constants).
const (
	BPhQueued     = BuildPhase(BuildQueued)
	BPhRunning    = BuildPhase(BuildRunning)
	BPhReleasing  = BuildPhase(BuildReleasing)
	BPhSucceeded  = BuildPhase(BuildSucceeded)
	BPhFailed     = BuildPhase(BuildFailed)
	BPhCancelling = BuildPhase(BuildCancelling)
	BPhCancelled  = BuildPhase(BuildCancelled)
)

// Typed event constants.
const (
	BPEventStarted          = BuildPhaseEvent(BuildStarted)
	BPEventSucceeded        = BuildPhaseEvent(BuildSucceededEvent)
	BPEventReleaseCompleted = BuildPhaseEvent(BuildReleaseCompleted)
	BPEventReleaseFailed    = BuildPhaseEvent(BuildReleaseFailedEvent)
	BPEventReleaseTimeout   = BuildPhaseEvent(BuildReleaseTimeoutEvent)
	BPEventFailed           = BuildPhaseEvent(BuildFailedEvent)
	BPEventCancelRequested  = BuildPhaseEvent(BuildCancelRequested)
	BPEventCancelConfirmed  = BuildPhaseEvent(BuildCancelConfirmed)
	BPEventQueueTimeout     = BuildPhaseEvent(BuildQueueTimeout)
	BPEventTimeout          = BuildPhaseEvent(BuildTimeout)
	BPEventCancelTimeout    = BuildPhaseEvent(BuildCancelTimeout)
)

// BuildPhaseTimeouts mirrors the package-level untyped timeout
// constants in typed form. Same values; the duplication keeps the
// typed surface complete without forcing callers to mix typed +
// untyped imports.
const (
	BPTimeoutQueued     = BuildQueuedTimeout
	BPTimeoutRunning    = BuildRunningTimeout
	BPTimeoutReleasing  = BuildReleasingTimeout
	BPTimeoutCancelling = BuildCancellingTimeout
)

// IsBuildPhaseTerminal returns true if the given phase is terminal.
// Typed equivalent of the package-level IsBuildTerminal.
func IsBuildPhaseTerminal(phase BuildPhase) bool {
	return IsBuildTerminal(State(phase))
}

// BuildMachine is the typed BuildPhase machine.
type BuildMachine = Typed[BuildPhase, BuildPhaseEvent]

// NewBuildTypedMachine returns the typed machine over NewBuildMachine(), which stays the
// single source of the transition table and the terminal set.
func NewBuildTypedMachine() *BuildMachine {
	return newTyped[BuildPhase, BuildPhaseEvent](NewBuildMachine())
}
