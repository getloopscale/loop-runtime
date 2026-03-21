package statemachine

import (
	"testing"
	"time"
)

func TestBuildMachine_HappyPath(t *testing.T) {
	m := NewBuildMachine()

	// QUEUED -> RUNNING -> SUCCEEDED
	state := m.Transition(BuildQueued, BuildStarted)
	assertState(t, state, BuildRunning)

	state = m.Transition(state, BuildSucceededEvent)
	assertState(t, state, BuildSucceeded)
}

func TestBuildMachine_BuildFails(t *testing.T) {
	m := NewBuildMachine()

	state := m.Transition(BuildQueued, BuildStarted)
	assertState(t, state, BuildRunning)

	state = m.Transition(state, BuildFailedEvent)
	assertState(t, state, BuildFailed)
}

func TestBuildMachine_QueuedBuildFails_SubmitError(t *testing.T) {
	// REVIEW-BLOCKER-1: A QUEUED build can fail immediately if workflow
	// submission fails. This transition allows the orchestrator to mark
	// the build as FAILED without waiting for the 5min queue timeout.
	m := NewBuildMachine()

	state := m.Transition(BuildQueued, BuildFailedEvent)
	assertState(t, state, BuildFailed)
}

func TestBuildMachine_CancelWhileQueued(t *testing.T) {
	m := NewBuildMachine()

	state := m.Transition(BuildQueued, BuildCancelRequested)
	assertState(t, state, BuildCancelled)
}

func TestBuildMachine_CancelWhileRunning(t *testing.T) {
	m := NewBuildMachine()

	state := m.Transition(BuildQueued, BuildStarted)
	state = m.Transition(state, BuildCancelRequested)
	assertState(t, state, BuildCancelling)

	state = m.Transition(state, BuildCancelConfirmed)
	assertState(t, state, BuildCancelled)
}

func TestBuildMachine_CancelTimeout(t *testing.T) {
	m := NewBuildMachine()

	state := m.Transition(BuildQueued, BuildStarted)
	state = m.Transition(state, BuildCancelRequested)
	assertState(t, state, BuildCancelling)

	state = m.Transition(state, BuildCancelTimeout)
	assertState(t, state, BuildCancelled)
}

func TestBuildMachine_QueueTimeout(t *testing.T) {
	m := NewBuildMachine()

	state := m.Transition(BuildQueued, BuildQueueTimeout)
	assertState(t, state, BuildFailed)
}

func TestBuildMachine_BuildTimeout(t *testing.T) {
	m := NewBuildMachine()

	state := m.Transition(BuildQueued, BuildStarted)
	state = m.Transition(state, BuildTimeout)
	assertState(t, state, BuildFailed)
}

func TestBuildMachine_TerminalStatesRejectAllEvents(t *testing.T) {
	m := NewBuildMachine()

	terminalStates := []State{BuildSucceeded, BuildFailed, BuildCancelled}
	allEvents := []Event{
		BuildStarted, BuildSucceededEvent, BuildFailedEvent,
		BuildCancelRequested, BuildCancelConfirmed,
		BuildQueueTimeout, BuildTimeout, BuildCancelTimeout,
	}

	for _, terminal := range terminalStates {
		for _, event := range allEvents {
			assertPanics(t, func() {
				m.Transition(terminal, event)
			}, "expected panic for transition from terminal state %q on event %q", terminal, event)
		}
	}
}

func TestBuildMachine_InvalidTransition_RunningToQueued(t *testing.T) {
	m := NewBuildMachine()

	assertPanics(t, func() {
		m.Transition(BuildRunning, BuildStarted)
	}, "RUNNING -> BuildStarted should panic (no RUNNING->QUEUED path)")
}

func TestBuildMachine_IsTerminal(t *testing.T) {
	m := NewBuildMachine()

	if m.IsTerminal(BuildQueued) {
		t.Error("QUEUED should not be terminal")
	}
	if m.IsTerminal(BuildRunning) {
		t.Error("RUNNING should not be terminal")
	}
	if m.IsTerminal(BuildCancelling) {
		t.Error("CANCELLING should not be terminal")
	}
	if !m.IsTerminal(BuildSucceeded) {
		t.Error("SUCCEEDED should be terminal")
	}
	if !m.IsTerminal(BuildFailed) {
		t.Error("FAILED should be terminal")
	}
	if !m.IsTerminal(BuildCancelled) {
		t.Error("CANCELLED should be terminal")
	}
}

func TestBuildMachine_Timeouts(t *testing.T) {
	m := NewBuildMachine()
	timeouts := m.Timeouts()

	expected := map[State]time.Duration{
		BuildQueued:     5 * time.Minute,
		BuildRunning:    30 * time.Minute,
		BuildCancelling: 60 * time.Second,
	}

	if len(timeouts) != len(expected) {
		t.Fatalf("expected %d timeouts, got %d", len(expected), len(timeouts))
	}

	for _, tc := range timeouts {
		exp, ok := expected[tc.State]
		if !ok {
			t.Errorf("unexpected timeout for state %q", tc.State)
			continue
		}
		if tc.Timeout != exp {
			t.Errorf("timeout for %q: expected %v, got %v", tc.State, exp, tc.Timeout)
		}
	}
}

func TestBuildMachine_Name(t *testing.T) {
	m := NewBuildMachine()
	if m.Name() != "Build" {
		t.Errorf("expected name %q, got %q", "Build", m.Name())
	}
}

func TestIsBuildTerminal(t *testing.T) {
	tests := []struct {
		state State
		want  bool
	}{
		{BuildSucceeded, true},
		{BuildFailed, true},
		{BuildCancelled, true},
		{BuildQueued, false},
		{BuildRunning, false},
		{BuildCancelling, false},
		{State("UNKNOWN"), false},
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := IsBuildTerminal(tt.state); got != tt.want {
				t.Errorf("IsBuildTerminal(%q) = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}
