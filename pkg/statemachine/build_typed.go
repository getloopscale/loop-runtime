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
	BPhQueued     BuildPhase = "QUEUED"
	BPhRunning    BuildPhase = "RUNNING"
	BPhReleasing  BuildPhase = "RELEASING"
	BPhSucceeded  BuildPhase = "SUCCEEDED"
	BPhFailed     BuildPhase = "FAILED"
	BPhCancelling BuildPhase = "CANCELLING"
	BPhCancelled  BuildPhase = "CANCELLED"
)

// Typed event constants.
const (
	BPEventStarted          BuildPhaseEvent = "BuildStarted"
	BPEventSucceeded        BuildPhaseEvent = "BuildSucceeded"
	BPEventReleaseCompleted BuildPhaseEvent = "BuildReleaseCompleted"
	BPEventReleaseFailed    BuildPhaseEvent = "BuildReleaseFailed"
	BPEventReleaseTimeout   BuildPhaseEvent = "BuildReleaseTimeout"
	BPEventFailed           BuildPhaseEvent = "BuildFailed"
	BPEventCancelRequested  BuildPhaseEvent = "BuildCancelRequested"
	BPEventCancelConfirmed  BuildPhaseEvent = "BuildCancelConfirmed"
	BPEventQueueTimeout     BuildPhaseEvent = "BuildQueueTimeout"
	BPEventTimeout          BuildPhaseEvent = "BuildTimeout"
	BPEventCancelTimeout    BuildPhaseEvent = "BuildCancelTimeout"
)

// BuildMachine is the typed wrapper around the shared *Machine
// substrate. Delegates all operations after a same-bit-pattern cast
// to/from the untyped State/Event types.
type BuildMachine struct {
	inner *Machine
}

// NewBuildTypedMachine constructs the typed Build state machine.
// Internally reuses NewBuildMachine() so the transition table +
// terminal set stay single-sourced.
func NewBuildTypedMachine() *BuildMachine {
	return &BuildMachine{inner: NewBuildMachine()}
}

// Transition runs a typed transition. Panics on invalid input.
func (m *BuildMachine) Transition(current BuildPhase, event BuildPhaseEvent) BuildPhase {
	return BuildPhase(m.inner.Transition(State(current), Event(event)))
}

// TryTransition is the non-panicking variant.
func (m *BuildMachine) TryTransition(current BuildPhase, event BuildPhaseEvent) (BuildPhase, error) {
	next, err := m.inner.TryTransition(State(current), Event(event))
	if err != nil {
		return "", err
	}
	return BuildPhase(next), nil
}

// IsTerminal reports whether the given phase is terminal (SUCCEEDED,
// FAILED, CANCELLED).
func (m *BuildMachine) IsTerminal(p BuildPhase) bool {
	return m.inner.IsTerminal(State(p))
}

// Name returns "Build".
func (m *BuildMachine) Name() string {
	return m.inner.Name()
}

// IsKnownState mirrors Machine.IsKnownState.
func (m *BuildMachine) IsKnownState(p BuildPhase) bool {
	return m.inner.IsKnownState(State(p))
}

// IsKnownEvent mirrors Machine.IsKnownEvent.
func (m *BuildMachine) IsKnownEvent(e BuildPhaseEvent) bool {
	return m.inner.IsKnownEvent(Event(e))
}

// MustParseState wraps a raw string into a BuildPhase after
// validating membership. Panics on unknown values.
func (m *BuildMachine) MustParseState(s string) BuildPhase {
	return BuildPhase(m.inner.MustParseState(s))
}

// MustParseEvent wraps a raw string into a BuildPhaseEvent.
func (m *BuildMachine) MustParseEvent(e string) BuildPhaseEvent {
	return BuildPhaseEvent(m.inner.MustParseEvent(e))
}

// ValidEvents returns the typed events valid from the given phase.
func (m *BuildMachine) ValidEvents(phase BuildPhase) []BuildPhaseEvent {
	untyped := m.inner.ValidEvents(State(phase))
	if untyped == nil {
		return nil
	}
	out := make([]BuildPhaseEvent, len(untyped))
	for i, e := range untyped {
		out[i] = BuildPhaseEvent(e)
	}
	return out
}

// Timeouts returns the timeout config for each Build phase.
// Re-exposed in typed form for callers that previously read it from
// the untyped *Machine directly.
func (m *BuildMachine) Timeouts() []TimeoutConfig {
	return m.inner.Timeouts()
}

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
	switch phase {
	case BPhSucceeded, BPhFailed, BPhCancelled:
		return true
	}
	return false
}
