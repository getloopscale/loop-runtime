package statemachine

import (
	"fmt"
	"testing"
)

// assertState is a test helper that checks the state matches the expected value.
func assertState(t *testing.T, got, want State) {
	t.Helper()
	if got != want {
		t.Errorf("expected state %q, got %q", want, got)
	}
}

// assertPanics is a test helper that verifies the given function panics.
func assertPanics(t *testing.T, f func(), msgAndArgs ...interface{}) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			msg := "expected panic but did not get one"
			if len(msgAndArgs) > 0 {
				msg = fmt.Sprintf(msgAndArgs[0].(string), msgAndArgs[1:]...)
			}
			t.Error(msg)
		}
	}()
	f()
}

func TestMachine_PanicsOnDuplicateTransition(t *testing.T) {
	assertPanics(t, func() {
		NewMachine(MachineConfig{
			Name: "Test",
			Transitions: []transition{
				{From: "A", Event: "go", To: "B"},
				{From: "A", Event: "go", To: "C"}, // duplicate
			},
			Terminal: []State{"B", "C"},
		})
	}, "expected panic on duplicate transition")
}

func TestMachine_PanicsOnTerminalWithOutgoingTransition(t *testing.T) {
	assertPanics(t, func() {
		NewMachine(MachineConfig{
			Name: "Test",
			Transitions: []transition{
				{From: "DONE", Event: "go", To: "B"}, // DONE is terminal
			},
			Terminal: []State{"DONE"},
		})
	}, "expected panic when terminal state has outgoing transition")
}

func TestMachine_ValidEvents(t *testing.T) {
	m := NewMachine(MachineConfig{
		Name: "Test",
		Transitions: []transition{
			{From: "A", Event: "go", To: "B"},
			{From: "A", Event: "skip", To: "C"},
		},
		Terminal: []State{"B", "C"},
	})

	events := m.ValidEvents("A")
	if len(events) != 2 {
		t.Fatalf("expected 2 valid events from A, got %d", len(events))
	}

	// Terminal states have no valid events.
	events = m.ValidEvents("B")
	if events != nil {
		t.Errorf("expected nil events from terminal state B, got %v", events)
	}

	// Unknown states have no valid events.
	events = m.ValidEvents("UNKNOWN")
	if events != nil {
		t.Errorf("expected nil events from unknown state, got %v", events)
	}
}

func TestMachine_TransitionPanicsOnUnknownState(t *testing.T) {
	m := NewMachine(MachineConfig{
		Name: "Test",
		Transitions: []transition{
			{From: "A", Event: "go", To: "B"},
		},
		Terminal: []State{"B"},
	})

	assertPanics(t, func() {
		m.Transition("UNKNOWN", "go")
	}, "expected panic on transition from unknown state")
}

func TestMachine_TransitionPanicsOnInvalidEvent(t *testing.T) {
	m := NewMachine(MachineConfig{
		Name: "Test",
		Transitions: []transition{
			{From: "A", Event: "go", To: "B"},
		},
		Terminal: []State{"B"},
	})

	assertPanics(t, func() {
		m.Transition("A", "invalid_event")
	}, "expected panic on invalid event")
}
