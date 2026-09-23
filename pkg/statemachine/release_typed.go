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
	RPhReady           = ReleasePhase(ReleaseReady)
	RPhPending         = ReleasePhase(ReleasePending)
	RPhActive          = ReleasePhase(ReleaseActive)
	RPhGitMirrorFailed = ReleasePhase(ReleaseGitMirrorFailed)
	RPhSyncFailed      = ReleasePhase(ReleaseSyncFailed)
	RPhDriftAlert      = ReleasePhase(ReleaseDriftAlert)
	RPhSuperseded      = ReleasePhase(ReleaseSuperseded)
	RPhRollingBack     = ReleasePhase(ReleaseRollingBack)
	RPhRolledBack      = ReleasePhase(ReleaseRolledBack)
	RPhRollbackFailed  = ReleasePhase(ReleaseRollbackFailed)
	RPhCancelled       = ReleasePhase(ReleaseCancelled)
	// RPhArchived per T4.5 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap.
	// Mirrors ReleaseArchived (introduced by migration 000008).
	RPhArchived = ReleasePhase(ReleaseArchived)
)

// Typed event constants.
const (
	RPEventGitMirrorCompleted = ReleasePhaseEvent(ReleaseGitMirrorCompleted)
	RPEventGitMirrorFailed    = ReleasePhaseEvent(ReleaseGitMirrorFailed_)
	RPEventGitMirrorRetried   = ReleasePhaseEvent(ReleaseGitMirrorRetried)
	RPEventSyncCompleted      = ReleasePhaseEvent(ReleaseSyncCompleted)
	RPEventSyncFailed         = ReleasePhaseEvent(ReleaseSyncFailed_)
	RPEventSyncExhausted      = ReleasePhaseEvent(ReleaseSyncExhausted)
	RPEventSyncRetry          = ReleasePhaseEvent(ReleaseSyncRetry)
	RPEventNewActivated       = ReleasePhaseEvent(ReleaseNewActivated)
	RPEventRollbackStarted    = ReleasePhaseEvent(ReleaseRollbackStarted)
	RPEventRollbackCompleted  = ReleasePhaseEvent(ReleaseRollbackCompleted)
	RPEventRollbackFailed     = ReleasePhaseEvent(ReleaseRollbackFailed_)
	RPEventCancelled          = ReleasePhaseEvent(ReleaseCancelledEvent)
	RPEventGitPushTimeout     = ReleasePhaseEvent(ReleaseGitPushTimeout)
	RPEventSyncTimeout        = ReleasePhaseEvent(ReleaseSyncTimeout)
	RPEventRollbackTimeout    = ReleasePhaseEvent(ReleaseRollbackTimeout)
	RPEventManualResolve      = ReleasePhaseEvent(ReleaseManualResolve)
	RPEventReactivated        = ReleasePhaseEvent(ReleaseReactivated)
	RPEventProjectDeleted     = ReleasePhaseEvent(ReleaseProjectDeleted)
	// RPEventArchived per T4.5; mirrors ReleaseArchiveEvent.
	RPEventArchived = ReleasePhaseEvent(ReleaseArchiveEvent)
)

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
	return IsReleaseTerminal(State(phase))
}

// IsReleasePhaseReapable mirrors the untyped IsReleaseReapable
// (only READY + PENDING are reapable; anything else is forward
// progress).
func IsReleasePhaseReapable(phase ReleasePhase) bool {
	return IsReleaseReapable(State(phase))
}

// ReleaseMachine is the typed ReleasePhase machine.
type ReleaseMachine = Typed[ReleasePhase, ReleasePhaseEvent]

// NewReleaseTypedMachine returns the typed machine over NewReleaseMachine(), which stays the
// single source of the transition table and the terminal set.
func NewReleaseTypedMachine() *ReleaseMachine {
	return newTyped[ReleasePhase, ReleasePhaseEvent](NewReleaseMachine())
}
