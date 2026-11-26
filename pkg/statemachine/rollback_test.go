// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

import "testing"

func TestRollbackMachine_HappyPath(t *testing.T) {
	m := NewRollbackMachine()

	// VALIDATING -> REVERTING -> SYNCING -> COMPLETED
	state := m.Transition(RollbackValidating, RollbackValidationPassed)
	assertState(t, state, RollbackReverting)

	state = m.Transition(state, RollbackRevertCompleted)
	assertState(t, state, RollbackSyncing)

	state = m.Transition(state, RollbackSyncCompleted)
	assertState(t, state, RollbackCompleted)
}

func TestRollbackMachine_Rejected(t *testing.T) {
	m := NewRollbackMachine()

	state := m.Transition(RollbackValidating, RollbackUnacknowledgedEffects)
	assertState(t, state, RollbackRejected)
}

func TestRollbackMachine_Blocked(t *testing.T) {
	m := NewRollbackMachine()

	state := m.Transition(RollbackValidating, RollbackNoPreviousRelease)
	assertState(t, state, RollbackBlocked)
}

func TestRollbackMachine_RevertFailed(t *testing.T) {
	m := NewRollbackMachine()

	state := m.Transition(RollbackValidating, RollbackValidationPassed)
	state = m.Transition(state, RollbackRevertFailed_)
	assertState(t, state, RollbackRevertFailed)
}

func TestRollbackMachine_SyncFailed(t *testing.T) {
	m := NewRollbackMachine()

	state := m.Transition(RollbackValidating, RollbackValidationPassed)
	state = m.Transition(state, RollbackRevertCompleted)
	state = m.Transition(state, RollbackSyncFailed_)
	assertState(t, state, RollbackSyncFailed)
}

func TestRollbackMachine_TerminalStatesRejectAllEvents(t *testing.T) {
	m := NewRollbackMachine()

	terminalStates := []State{RollbackCompleted, RollbackRejected, RollbackBlocked, RollbackRevertFailed, RollbackSyncFailed}
	allEvents := []Event{
		RollbackValidationPassed, RollbackUnacknowledgedEffects,
		RollbackNoPreviousRelease, RollbackRevertCompleted,
		RollbackRevertFailed_, RollbackSyncCompleted, RollbackSyncFailed_,
	}

	for _, terminal := range terminalStates {
		for _, event := range allEvents {
			assertPanics(t, func() {
				m.Transition(terminal, event)
			}, "expected panic for transition from terminal state %q on event %q", terminal, event)
		}
	}
}

func TestRollbackMachine_IsTerminal(t *testing.T) {
	m := NewRollbackMachine()

	terminal := []State{RollbackCompleted, RollbackRejected, RollbackBlocked, RollbackRevertFailed, RollbackSyncFailed}
	for _, s := range terminal {
		if !m.IsTerminal(s) {
			t.Errorf("state %q should be terminal", s)
		}
	}

	nonTerminal := []State{RollbackValidating, RollbackReverting, RollbackSyncing}
	for _, s := range nonTerminal {
		if m.IsTerminal(s) {
			t.Errorf("state %q should not be terminal", s)
		}
	}
}

func TestRollbackMachine_SyncFailedIsTerminal(t *testing.T) {
	// SYNC_FAILED is terminal: rollbacks are user-initiated operations.
	// If sync fails, the user should evaluate and issue a new rollback.
	// Consistent with REVERT_FAILED being terminal.
	m := NewRollbackMachine()
	if !m.IsTerminal(RollbackSyncFailed) {
		t.Error("SYNC_FAILED should be terminal")
	}
}
