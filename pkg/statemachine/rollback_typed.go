// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

// Phase 4 of ADR `2026-05-15-T6-1-typed-state-machines.md`:
// Rollback typed wrapper.

// RollbackPhase is the typed state for the Rollback lifecycle.
type RollbackPhase string

// RollbackPhaseEvent is the typed event for the Rollback lifecycle.
type RollbackPhaseEvent string

const (
	RbPhValidating   = RollbackPhase(RollbackValidating)
	RbPhReverting    = RollbackPhase(RollbackReverting)
	RbPhSyncing      = RollbackPhase(RollbackSyncing)
	RbPhCompleted    = RollbackPhase(RollbackCompleted)
	RbPhRejected     = RollbackPhase(RollbackRejected)
	RbPhBlocked      = RollbackPhase(RollbackBlocked)
	RbPhRevertFailed = RollbackPhase(RollbackRevertFailed)
	RbPhSyncFailed   = RollbackPhase(RollbackSyncFailed)
)

const (
	RbPEventValidationPassed      = RollbackPhaseEvent(RollbackValidationPassed)
	RbPEventUnacknowledgedEffects = RollbackPhaseEvent(RollbackUnacknowledgedEffects)
	RbPEventNoPreviousRelease     = RollbackPhaseEvent(RollbackNoPreviousRelease)
	RbPEventRevertCompleted       = RollbackPhaseEvent(RollbackRevertCompleted)
	RbPEventRevertFailed          = RollbackPhaseEvent(RollbackRevertFailed_)
	RbPEventSyncCompleted         = RollbackPhaseEvent(RollbackSyncCompleted)
	RbPEventSyncFailed            = RollbackPhaseEvent(RollbackSyncFailed_)
)

// RollbackTypedMachine is the typed RollbackPhase machine.
type RollbackTypedMachine = Typed[RollbackPhase, RollbackPhaseEvent]

// NewRollbackTypedMachine returns the typed machine over NewRollbackMachine(), which stays the
// single source of the transition table and the terminal set.
func NewRollbackTypedMachine() *RollbackTypedMachine {
	return newTyped[RollbackPhase, RollbackPhaseEvent](NewRollbackMachine())
}
