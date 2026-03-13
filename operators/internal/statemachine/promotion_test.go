package statemachine

import "testing"

func TestPromotionMachine_HappyPath(t *testing.T) {
	m := NewPromotionMachine()

	// VALIDATING -> COMMITTING -> SYNCING -> COMPLETED
	state := m.Transition(PromotionValidating, PromotionValidationPassed)
	assertState(t, state, PromotionCommitting)

	state = m.Transition(state, PromotionCommitCompleted)
	assertState(t, state, PromotionSyncing)

	state = m.Transition(state, PromotionSyncCompleted)
	assertState(t, state, PromotionCompleted)
}

func TestPromotionMachine_Rejected(t *testing.T) {
	m := NewPromotionMachine()

	state := m.Transition(PromotionValidating, PromotionValidationFailed)
	assertState(t, state, PromotionRejected)
}

func TestPromotionMachine_CommitFailed(t *testing.T) {
	m := NewPromotionMachine()

	state := m.Transition(PromotionValidating, PromotionValidationPassed)
	state = m.Transition(state, PromotionCommitFailed_)
	assertState(t, state, PromotionCommitFailed)
}

func TestPromotionMachine_SyncFailed(t *testing.T) {
	m := NewPromotionMachine()

	state := m.Transition(PromotionValidating, PromotionValidationPassed)
	state = m.Transition(state, PromotionCommitCompleted)
	state = m.Transition(state, PromotionSyncFailed_)
	assertState(t, state, PromotionSyncFailed)
}

func TestPromotionMachine_TerminalStatesRejectAllEvents(t *testing.T) {
	m := NewPromotionMachine()

	terminalStates := []State{PromotionCompleted, PromotionRejected, PromotionCommitFailed}
	allEvents := []Event{
		PromotionValidationPassed, PromotionValidationFailed,
		PromotionCommitCompleted, PromotionCommitFailed_,
		PromotionSyncCompleted, PromotionSyncFailed_,
	}

	for _, terminal := range terminalStates {
		for _, event := range allEvents {
			assertPanics(t, func() {
				m.Transition(terminal, event)
			}, "expected panic for transition from terminal state %q on event %q", terminal, event)
		}
	}
}

func TestPromotionMachine_SyncFailedIsNotTerminal(t *testing.T) {
	// SYNC_FAILED is not listed as terminal in spec 5.4.
	m := NewPromotionMachine()
	if m.IsTerminal(PromotionSyncFailed) {
		t.Error("SYNC_FAILED should not be terminal")
	}
}

func TestPromotionMachine_IsTerminal(t *testing.T) {
	m := NewPromotionMachine()

	if !m.IsTerminal(PromotionCompleted) {
		t.Error("COMPLETED should be terminal")
	}
	if !m.IsTerminal(PromotionRejected) {
		t.Error("REJECTED should be terminal")
	}
	if !m.IsTerminal(PromotionCommitFailed) {
		t.Error("COMMIT_FAILED should be terminal")
	}
}
