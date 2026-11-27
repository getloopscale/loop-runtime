// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

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

func TestMachine_TryTransition_ValidTransition(t *testing.T) {
	m := NewMachine(MachineConfig{
		Name: "Test",
		Transitions: []transition{
			{From: "A", Event: "go", To: "B"},
		},
		Terminal: []State{"B"},
	})

	next, err := m.TryTransition("A", "go")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	assertState(t, next, "B")
}

func TestMachine_TryTransition_TerminalState(t *testing.T) {
	m := NewMachine(MachineConfig{
		Name: "Test",
		Transitions: []transition{
			{From: "A", Event: "go", To: "B"},
		},
		Terminal: []State{"B"},
	})

	_, err := m.TryTransition("B", "go")
	if err == nil {
		t.Fatal("expected error for transition from terminal state, got nil")
	}
}

func TestMachine_TryTransition_UnknownState(t *testing.T) {
	m := NewMachine(MachineConfig{
		Name: "Test",
		Transitions: []transition{
			{From: "A", Event: "go", To: "B"},
		},
		Terminal: []State{"B"},
	})

	_, err := m.TryTransition("UNKNOWN", "go")
	if err == nil {
		t.Fatal("expected error for transition from unknown state, got nil")
	}
}

func TestMachine_TryTransition_InvalidEvent(t *testing.T) {
	m := NewMachine(MachineConfig{
		Name: "Test",
		Transitions: []transition{
			{From: "A", Event: "go", To: "B"},
		},
		Terminal: []State{"B"},
	})

	_, err := m.TryTransition("A", "invalid_event")
	if err == nil {
		t.Fatal("expected error for invalid event, got nil")
	}
}

func TestMachine_TryTransition_DoesNotPanic(t *testing.T) {
	m := NewMachine(MachineConfig{
		Name: "Test",
		Transitions: []transition{
			{From: "A", Event: "go", To: "B"},
		},
		Terminal: []State{"B"},
	})

	// Verify TryTransition never panics, even with completely bogus input.
	// This is the key difference from Transition().
	cases := []struct {
		state State
		event Event
	}{
		{"UNKNOWN", "go"},
		{"B", "go"},
		{"A", "invalid_event"},
		{"", ""},
	}
	for _, tc := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("TryTransition(%q, %q) panicked: %v", tc.state, tc.event, r)
				}
			}()
			_, _ = m.TryTransition(tc.state, tc.event)
		}()
	}
}

// =============================================================================
// MustParseState / MustParseEvent / IsKnownState / IsKnownEvent — audit M11 #25
// =============================================================================

// fixtureMachine is a small machine used across the validation helpers below.
// Transitions: A -> B (go), B -> C (go), C terminal.
func fixtureMachine() *Machine {
	return NewMachine(MachineConfig{
		Name: "fixture",
		Transitions: []transition{
			{From: "A", Event: "go", To: "B"},
			{From: "B", Event: "go", To: "C"},
		},
		Terminal: []State{"C"},
	})
}

func TestIsKnownState_CoversSourceDestinationTerminal(t *testing.T) {
	m := fixtureMachine()

	// Source state appears in transitions.From.
	if !m.IsKnownState("A") {
		t.Error("expected A to be known (source of A->B)")
	}
	// Destination-only state appears in transitions.To.
	if !m.IsKnownState("B") {
		t.Error("expected B to be known (destination of A->B and source of B->C)")
	}
	// Terminal state is explicitly registered.
	if !m.IsKnownState("C") {
		t.Error("expected C to be known (terminal)")
	}
	// Unknown state must NOT be reported as known.
	if m.IsKnownState("UNKNOWN") {
		t.Error("expected UNKNOWN to NOT be known")
	}
}

func TestIsKnownEvent(t *testing.T) {
	m := fixtureMachine()

	if !m.IsKnownEvent("go") {
		t.Error("expected 'go' to be a known event")
	}
	if m.IsKnownEvent("not-an-event") {
		t.Error("expected unknown event to NOT be known")
	}
}

func TestMustParseState_AcceptsKnown(t *testing.T) {
	m := fixtureMachine()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("MustParseState must not panic for known state; got panic=%v", r)
		}
	}()

	got := m.MustParseState("B")
	if got != "B" {
		t.Errorf("MustParseState returned %q, want %q", got, "B")
	}
}

func TestMustParseState_PanicsOnUnknown(t *testing.T) {
	m := fixtureMachine()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("MustParseState must panic for unknown state")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic payload not a string: %T = %v", r, r)
		}
		// The panic message must reference the audit gap so log search can
		// correlate this to the state-machine string-alias mitigation.
		if !contains(msg, "audit M11 #25") {
			t.Errorf("panic missing audit ref; got %q", msg)
		}
		if !contains(msg, "UNKNOWN") {
			t.Errorf("panic must include the offending value; got %q", msg)
		}
	}()

	_ = m.MustParseState("UNKNOWN")
}

func TestMustParseEvent_AcceptsKnown(t *testing.T) {
	m := fixtureMachine()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("MustParseEvent must not panic for known event; got panic=%v", r)
		}
	}()

	got := m.MustParseEvent("go")
	if got != "go" {
		t.Errorf("MustParseEvent returned %q, want %q", got, "go")
	}
}

func TestMustParseEvent_PanicsOnUnknown(t *testing.T) {
	m := fixtureMachine()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("MustParseEvent must panic for unknown event")
		}
		msg, _ := r.(string)
		if !contains(msg, "audit M11 #25") {
			t.Errorf("panic missing audit ref; got %q", msg)
		}
	}()

	_ = m.MustParseEvent("invalid-event")
}

// Local contains helper to avoid bringing in strings just for one call site.
// (machine_test.go already uses fmt only; keeping this minimal.)
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
