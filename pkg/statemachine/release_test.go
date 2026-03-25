package statemachine

import "testing"

func TestReleaseMachine_HappyPath(t *testing.T) {
	m := NewReleaseMachine()

	// READY -> PENDING -> ACTIVE -> SUPERSEDED
	state := m.Transition(ReleaseReady, ReleaseGitMirrorCompleted)
	assertState(t, state, ReleasePending)

	state = m.Transition(state, ReleaseSyncCompleted)
	assertState(t, state, ReleaseActive)

	state = m.Transition(state, ReleaseNewActivated)
	assertState(t, state, ReleaseSuperseded)
}

func TestReleaseMachine_GitMirrorFailedThenRetry(t *testing.T) {
	m := NewReleaseMachine()

	state := m.Transition(ReleaseReady, ReleaseGitMirrorFailed_)
	assertState(t, state, ReleaseGitMirrorFailed)

	state = m.Transition(state, ReleaseGitMirrorRetried)
	assertState(t, state, ReleasePending)
}

func TestReleaseMachine_GitPushTimeout(t *testing.T) {
	m := NewReleaseMachine()

	state := m.Transition(ReleaseReady, ReleaseGitPushTimeout)
	assertState(t, state, ReleaseGitMirrorFailed)
}

func TestReleaseMachine_SyncFailedThenRetry(t *testing.T) {
	m := NewReleaseMachine()

	state := m.Transition(ReleaseReady, ReleaseGitMirrorCompleted)
	state = m.Transition(state, ReleaseSyncFailed_)
	assertState(t, state, ReleaseSyncFailed)

	state = m.Transition(state, ReleaseSyncRetry)
	assertState(t, state, ReleasePending)
}

func TestReleaseMachine_SyncExhaustedToDriftAlert(t *testing.T) {
	m := NewReleaseMachine()

	state := m.Transition(ReleaseReady, ReleaseGitMirrorCompleted)
	state = m.Transition(state, ReleaseSyncFailed_)
	state = m.Transition(state, ReleaseSyncExhausted)
	assertState(t, state, ReleaseDriftAlert)
}

func TestReleaseMachine_SyncTimeout(t *testing.T) {
	m := NewReleaseMachine()

	state := m.Transition(ReleaseReady, ReleaseGitMirrorCompleted)
	state = m.Transition(state, ReleaseSyncTimeout)
	assertState(t, state, ReleaseSyncFailed)
}

func TestReleaseMachine_PendingCancelled(t *testing.T) {
	m := NewReleaseMachine()

	state := m.Transition(ReleaseReady, ReleaseGitMirrorCompleted)
	state = m.Transition(state, ReleaseCancelledEvent)
	assertState(t, state, ReleaseCancelled)
}

func TestReleaseMachine_RollbackHappyPath(t *testing.T) {
	m := NewReleaseMachine()

	state := m.Transition(ReleaseReady, ReleaseGitMirrorCompleted)
	state = m.Transition(state, ReleaseSyncCompleted)
	state = m.Transition(state, ReleaseRollbackStarted)
	assertState(t, state, ReleaseRollingBack)

	state = m.Transition(state, ReleaseRollbackCompleted)
	assertState(t, state, ReleaseRolledBack)
}

func TestReleaseMachine_RollbackFailed(t *testing.T) {
	m := NewReleaseMachine()

	state := m.Transition(ReleaseReady, ReleaseGitMirrorCompleted)
	state = m.Transition(state, ReleaseSyncCompleted)
	state = m.Transition(state, ReleaseRollbackStarted)
	state = m.Transition(state, ReleaseRollbackFailed_)
	assertState(t, state, ReleaseRollbackFailed)
}

func TestReleaseMachine_RollbackTimeout(t *testing.T) {
	m := NewReleaseMachine()

	state := m.Transition(ReleaseReady, ReleaseGitMirrorCompleted)
	state = m.Transition(state, ReleaseSyncCompleted)
	state = m.Transition(state, ReleaseRollbackStarted)
	state = m.Transition(state, ReleaseRollbackTimeout)
	assertState(t, state, ReleaseRollbackFailed)
}

func TestReleaseMachine_TerminalStatesRejectAllEvents(t *testing.T) {
	m := NewReleaseMachine()

	terminalStates := []State{ReleaseSuperseded, ReleaseRolledBack, ReleaseCancelled}
	sampleEvents := []Event{
		ReleaseGitMirrorCompleted, ReleaseSyncCompleted,
		ReleaseNewActivated, ReleaseRollbackStarted,
	}

	for _, terminal := range terminalStates {
		for _, event := range sampleEvents {
			assertPanics(t, func() {
				m.Transition(terminal, event)
			}, "expected panic for transition from terminal state %q on event %q", terminal, event)
		}
	}
}

func TestReleaseMachine_IsTerminal(t *testing.T) {
	m := NewReleaseMachine()

	// SUPERSEDED and ROLLED_BACK are NOT terminal — they can be reactivated via rollback.
	nonTerminal := []State{
		ReleaseReady, ReleasePending, ReleaseActive,
		ReleaseGitMirrorFailed, ReleaseSyncFailed, ReleaseDriftAlert,
		ReleaseRollingBack, ReleaseRollbackFailed,
		ReleaseSuperseded, ReleaseRolledBack,
	}
	for _, s := range nonTerminal {
		if m.IsTerminal(s) {
			t.Errorf("state %q should not be terminal", s)
		}
	}

	// Only CANCELLED is truly terminal (cannot be recovered).
	terminal := []State{ReleaseCancelled}
	for _, s := range terminal {
		if !m.IsTerminal(s) {
			t.Errorf("state %q should be terminal", s)
		}
	}
}

func TestReleaseMachine_DriftAlertIsNotTerminal(t *testing.T) {
	// DRIFT_ALERT requires manual intervention but is NOT terminal per spec.
	// The spec says it requires manual intervention, but does not list it as terminal.
	// It has no outgoing transitions defined, so it is a sink state.
	m := NewReleaseMachine()
	if m.IsTerminal(ReleaseDriftAlert) {
		t.Error("DRIFT_ALERT should not be terminal (requires manual intervention, not terminal)")
	}
}

func TestReleaseMachine_RollbackFailedIsNotTerminal(t *testing.T) {
	// ROLLBACK_FAILED requires manual intervention but is NOT terminal per spec.
	m := NewReleaseMachine()
	if m.IsTerminal(ReleaseRollbackFailed) {
		t.Error("ROLLBACK_FAILED should not be terminal (requires manual intervention)")
	}
}

func TestReleaseMachine_Reactivation(t *testing.T) {
	m := NewReleaseMachine()

	t.Run("SUPERSEDED can be reactivated to ACTIVE", func(t *testing.T) {
		next := m.Transition(ReleaseSuperseded, ReleaseReactivated)
		if next != ReleaseActive {
			t.Errorf("expected ACTIVE, got %s", next)
		}
	})

	t.Run("ROLLED_BACK can be reactivated to ACTIVE", func(t *testing.T) {
		next := m.Transition(ReleaseRolledBack, ReleaseReactivated)
		if next != ReleaseActive {
			t.Errorf("expected ACTIVE, got %s", next)
		}
	})

	t.Run("CANCELLED cannot be reactivated", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for reactivating CANCELLED release")
			}
		}()
		m.Transition(ReleaseCancelled, ReleaseReactivated)
	})
}
