// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

// Phase 2 of ADR `2026-05-15-T6-1-typed-state-machines.md` continued.
// Build typed wrapper shipped in build_typed.go; this file is the
// Release sibling.
//
// Same coexistence design as ExposedRoute + Build:
//   - distinct types `ReleasePhase string` + `ReleasePhaseEvent string`
//   - byte-for-byte string equality with untyped Release* constants
//   - typed Transition / TryTransition / IsTerminal / etc. methods
//   - untyped NewReleaseMachine() preserved for unmigrated callers

// ReleasePhase is the typed state for the Release lifecycle.
type ReleasePhase string

// ReleasePhaseEvent is the typed event for the Release lifecycle.
type ReleasePhaseEvent string

// Typed state constants (mirror the untyped ReleaseX constants).
const (
	RPhReady           ReleasePhase = "READY"
	RPhPending         ReleasePhase = "PENDING"
	RPhActive          ReleasePhase = "ACTIVE"
	RPhGitMirrorFailed ReleasePhase = "GIT_MIRROR_FAILED"
	RPhSyncFailed      ReleasePhase = "SYNC_FAILED"
	RPhDriftAlert      ReleasePhase = "DRIFT_ALERT"
	RPhSuperseded      ReleasePhase = "SUPERSEDED"
	RPhRollingBack     ReleasePhase = "ROLLING_BACK"
	RPhRolledBack      ReleasePhase = "ROLLED_BACK"
	RPhRollbackFailed  ReleasePhase = "ROLLBACK_FAILED"
	RPhCancelled       ReleasePhase = "CANCELLED"
	// RPhArchived per T4.5 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap.
	// Mirrors ReleaseArchived (introduced by migration 000008).
	RPhArchived ReleasePhase = "ARCHIVED"
)

// Typed event constants.
const (
	RPEventGitMirrorCompleted ReleasePhaseEvent = "GitMirrorCompleted"
	RPEventGitMirrorFailed    ReleasePhaseEvent = "GitMirrorFailed"
	RPEventGitMirrorRetried   ReleasePhaseEvent = "GitMirrorRetried"
	RPEventSyncCompleted      ReleasePhaseEvent = "SyncCompleted"
	RPEventSyncFailed         ReleasePhaseEvent = "SyncFailed"
	RPEventSyncExhausted      ReleasePhaseEvent = "SyncExhausted"
	RPEventSyncRetry          ReleasePhaseEvent = "SyncRetry"
	RPEventNewActivated       ReleasePhaseEvent = "NewReleaseActivated"
	RPEventRollbackStarted    ReleasePhaseEvent = "RollbackStarted"
	RPEventRollbackCompleted  ReleasePhaseEvent = "RollbackCompleted"
	RPEventRollbackFailed     ReleasePhaseEvent = "RollbackFailed"
	RPEventCancelled          ReleasePhaseEvent = "ReleaseCancelled"
	RPEventGitPushTimeout     ReleasePhaseEvent = "GitPushTimeout"
	RPEventSyncTimeout        ReleasePhaseEvent = "SyncTimeout"
	RPEventRollbackTimeout    ReleasePhaseEvent = "RollbackTimeout"
	RPEventManualResolve      ReleasePhaseEvent = "ManualResolve"
	RPEventReactivated        ReleasePhaseEvent = "Reactivated"
	RPEventProjectDeleted     ReleasePhaseEvent = "ProjectDeleted"
	// RPEventArchived per T4.5; mirrors ReleaseArchiveEvent.
	RPEventArchived ReleasePhaseEvent = "ReleaseArchived"
)

// ReleaseMachine is the typed wrapper around the shared *Machine.
type ReleaseMachine struct {
	inner *Machine
}

// NewReleaseTypedMachine constructs the typed Release state machine.
func NewReleaseTypedMachine() *ReleaseMachine {
	return &ReleaseMachine{inner: NewReleaseMachine()}
}

// Transition runs a typed transition. Panics on invalid input.
func (m *ReleaseMachine) Transition(current ReleasePhase, event ReleasePhaseEvent) ReleasePhase {
	return ReleasePhase(m.inner.Transition(State(current), Event(event)))
}

// TryTransition is the non-panicking variant.
func (m *ReleaseMachine) TryTransition(current ReleasePhase, event ReleasePhaseEvent) (ReleasePhase, error) {
	next, err := m.inner.TryTransition(State(current), Event(event))
	if err != nil {
		return "", err
	}
	return ReleasePhase(next), nil
}

// IsTerminal reports whether the given phase is terminal. Only
// CANCELLED is truly terminal (matches the untyped IsReleaseTerminal).
func (m *ReleaseMachine) IsTerminal(p ReleasePhase) bool {
	return m.inner.IsTerminal(State(p))
}

// Name returns "Release".
func (m *ReleaseMachine) Name() string {
	return m.inner.Name()
}

// IsKnownState mirrors Machine.IsKnownState.
func (m *ReleaseMachine) IsKnownState(p ReleasePhase) bool {
	return m.inner.IsKnownState(State(p))
}

// IsKnownEvent mirrors Machine.IsKnownEvent.
func (m *ReleaseMachine) IsKnownEvent(e ReleasePhaseEvent) bool {
	return m.inner.IsKnownEvent(Event(e))
}

// MustParseState wraps a raw string into a ReleasePhase.
func (m *ReleaseMachine) MustParseState(s string) ReleasePhase {
	return ReleasePhase(m.inner.MustParseState(s))
}

// MustParseEvent wraps a raw string into a ReleasePhaseEvent.
func (m *ReleaseMachine) MustParseEvent(e string) ReleasePhaseEvent {
	return ReleasePhaseEvent(m.inner.MustParseEvent(e))
}

// ValidEvents returns the typed events valid from the given phase.
func (m *ReleaseMachine) ValidEvents(phase ReleasePhase) []ReleasePhaseEvent {
	untyped := m.inner.ValidEvents(State(phase))
	if untyped == nil {
		return nil
	}
	out := make([]ReleasePhaseEvent, len(untyped))
	for i, e := range untyped {
		out[i] = ReleasePhaseEvent(e)
	}
	return out
}

// Timeouts returns the timeout config for each Release phase.
func (m *ReleaseMachine) Timeouts() []TimeoutConfig {
	return m.inner.Timeouts()
}

// RPTimeout* aliases mirror the untyped package-level timeout
// constants in typed surface.
const (
	RPTimeoutReady       = ReleaseReadyTimeout
	RPTimeoutPending     = ReleasePendingTimeout
	RPTimeoutRollingBack = ReleaseRollingBackTimeout
)

// IsReleasePhaseTerminal returns true if the phase is terminal.
// Typed equivalent of the package-level IsReleaseTerminal — CANCELLED
// and ARCHIVED are truly terminal (SUPERSEDED + ROLLED_BACK can
// reactivate via rollback; ARCHIVED cannot). T4.5 added ARCHIVED per
// migration 000008.
func IsReleasePhaseTerminal(phase ReleasePhase) bool {
	return phase == RPhCancelled || phase == RPhArchived
}

// IsReleasePhaseReapable mirrors the untyped IsReleaseReapable
// (only READY + PENDING are reapable; anything else is forward
// progress).
func IsReleasePhaseReapable(phase ReleasePhase) bool {
	switch phase {
	case RPhReady, RPhPending:
		return true
	}
	return false
}
