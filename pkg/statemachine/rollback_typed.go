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
	RbPhValidating   RollbackPhase = "VALIDATING"
	RbPhReverting    RollbackPhase = "REVERTING"
	RbPhSyncing      RollbackPhase = "SYNCING"
	RbPhCompleted    RollbackPhase = "COMPLETED"
	RbPhRejected     RollbackPhase = "REJECTED"
	RbPhBlocked      RollbackPhase = "BLOCKED"
	RbPhRevertFailed RollbackPhase = "REVERT_FAILED"
	RbPhSyncFailed   RollbackPhase = "SYNC_FAILED"
)

const (
	RbPEventValidationPassed     RollbackPhaseEvent = "ValidationPassed"
	RbPEventUnacknowledgedEffects RollbackPhaseEvent = "UnacknowledgedEffects"
	RbPEventNoPreviousRelease    RollbackPhaseEvent = "NoPreviousRelease"
	RbPEventRevertCompleted      RollbackPhaseEvent = "RevertCompleted"
	RbPEventRevertFailed         RollbackPhaseEvent = "RevertFailed"
	RbPEventSyncCompleted        RollbackPhaseEvent = "SyncCompleted"
	RbPEventSyncFailed           RollbackPhaseEvent = "SyncFailed"
)

// RollbackTypedMachine is the typed wrapper around the shared *Machine.
type RollbackTypedMachine struct {
	inner *Machine
}

func NewRollbackTypedMachine() *RollbackTypedMachine {
	return &RollbackTypedMachine{inner: NewRollbackMachine()}
}

func (m *RollbackTypedMachine) Transition(current RollbackPhase, event RollbackPhaseEvent) RollbackPhase {
	return RollbackPhase(m.inner.Transition(State(current), Event(event)))
}

func (m *RollbackTypedMachine) TryTransition(current RollbackPhase, event RollbackPhaseEvent) (RollbackPhase, error) {
	next, err := m.inner.TryTransition(State(current), Event(event))
	if err != nil {
		return "", err
	}
	return RollbackPhase(next), nil
}

func (m *RollbackTypedMachine) IsTerminal(p RollbackPhase) bool {
	return m.inner.IsTerminal(State(p))
}

func (m *RollbackTypedMachine) Name() string {
	return m.inner.Name()
}

func (m *RollbackTypedMachine) IsKnownState(p RollbackPhase) bool {
	return m.inner.IsKnownState(State(p))
}

func (m *RollbackTypedMachine) IsKnownEvent(e RollbackPhaseEvent) bool {
	return m.inner.IsKnownEvent(Event(e))
}

func (m *RollbackTypedMachine) MustParseState(s string) RollbackPhase {
	return RollbackPhase(m.inner.MustParseState(s))
}

func (m *RollbackTypedMachine) MustParseEvent(e string) RollbackPhaseEvent {
	return RollbackPhaseEvent(m.inner.MustParseEvent(e))
}

func (m *RollbackTypedMachine) ValidEvents(phase RollbackPhase) []RollbackPhaseEvent {
	untyped := m.inner.ValidEvents(State(phase))
	if untyped == nil {
		return nil
	}
	out := make([]RollbackPhaseEvent, len(untyped))
	for i, e := range untyped {
		out[i] = RollbackPhaseEvent(e)
	}
	return out
}
