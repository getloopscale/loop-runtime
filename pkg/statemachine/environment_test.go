// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

import (
	"testing"
)

func TestEnvironmentMachine_ValidTransitions(t *testing.T) {
	m := NewEnvironmentMachine()

	tests := []struct {
		name  string
		from  State
		event Event
		to    State
	}{
		{"empty to pending", EnvironmentEmpty, EnvironmentCreate, EnvironmentPending},
		{"pending to active", EnvironmentPending, EnvironmentProvisioned, EnvironmentActive},
		{"pending to deleting", EnvironmentPending, EnvironmentDeleteRequested, EnvironmentDeleting},
		{"active to deleting", EnvironmentActive, EnvironmentDeleteRequested, EnvironmentDeleting},
		// MEET-2026-07-02-002 MA-3: delete must be reachable from every
		// non-terminal state (a delete can arrive before the first successful
		// reconcile, when the phase is still Empty). Additive edge; Deleting
		// stays the sole terminal.
		{"empty to deleting", EnvironmentEmpty, EnvironmentDeleteRequested, EnvironmentDeleting},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.Transition(tt.from, tt.event)
			if got != tt.to {
				t.Fatalf("Transition(%q, %q) = %q, want %q", tt.from, tt.event, got, tt.to)
			}
		})
	}
}

func TestEnvironmentMachine_DeletingIsTerminal(t *testing.T) {
	m := NewEnvironmentMachine()
	if !m.IsTerminal(EnvironmentDeleting) {
		t.Fatal("expected Deleting to be terminal")
	}
}

func TestEnvironmentMachine_NonTerminalStates(t *testing.T) {
	m := NewEnvironmentMachine()
	for _, s := range []State{EnvironmentEmpty, EnvironmentPending, EnvironmentActive} {
		if m.IsTerminal(s) {
			t.Fatalf("expected %q to be non-terminal", s)
		}
	}
}

func TestEnvironmentMachine_InvalidTransitionPanics(t *testing.T) {
	m := NewEnvironmentMachine()

	tests := []struct {
		name  string
		from  State
		event Event
	}{
		{"deleting is terminal", EnvironmentDeleting, EnvironmentCreate},
		{"empty cannot be provisioned", EnvironmentEmpty, EnvironmentProvisioned},
		{"active cannot be provisioned again", EnvironmentActive, EnvironmentProvisioned},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic for Transition(%q, %q), got none", tt.from, tt.event)
				}
			}()
			m.Transition(tt.from, tt.event)
		})
	}
}

func TestEnvironmentMachine_Name(t *testing.T) {
	m := NewEnvironmentMachine()
	if m.Name() != "Environment" {
		t.Fatalf("expected name=Environment, got %q", m.Name())
	}
}
