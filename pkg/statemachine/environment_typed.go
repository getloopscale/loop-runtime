// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

// Phase 3 of ADR `2026-05-15-T6-1-typed-state-machines.md`:
// Environment typed wrapper. Coexists with untyped NewEnvironmentMachine().

// EnvironmentPhase is the typed state for the Environment lifecycle.
type EnvironmentPhase string

// EnvironmentPhaseEvent is the typed event for the Environment lifecycle.
type EnvironmentPhaseEvent string

const (
	EPhEmpty    EnvironmentPhase = ""
	EPhPending  EnvironmentPhase = "Pending"
	EPhActive   EnvironmentPhase = "Active"
	EPhFailed   EnvironmentPhase = "Failed"
	EPhDeleting EnvironmentPhase = "Deleting"
)

const (
	EPEventCreate          EnvironmentPhaseEvent = "Create"
	EPEventProvisioned     EnvironmentPhaseEvent = "Provisioned"
	EPEventProvisionFailed EnvironmentPhaseEvent = "ProvisionFailed"
	EPEventRetry           EnvironmentPhaseEvent = "Retry"
	EPEventDeleteRequested EnvironmentPhaseEvent = "DeleteRequested"
)

// EnvironmentTypedMachine is the typed wrapper around the shared *Machine.
type EnvironmentTypedMachine struct {
	inner *Machine
}

func NewEnvironmentTypedMachine() *EnvironmentTypedMachine {
	return &EnvironmentTypedMachine{inner: NewEnvironmentMachine()}
}

func (m *EnvironmentTypedMachine) Transition(current EnvironmentPhase, event EnvironmentPhaseEvent) EnvironmentPhase {
	return EnvironmentPhase(m.inner.Transition(State(current), Event(event)))
}

func (m *EnvironmentTypedMachine) TryTransition(current EnvironmentPhase, event EnvironmentPhaseEvent) (EnvironmentPhase, error) {
	next, err := m.inner.TryTransition(State(current), Event(event))
	if err != nil {
		return "", err
	}
	return EnvironmentPhase(next), nil
}

func (m *EnvironmentTypedMachine) IsTerminal(p EnvironmentPhase) bool {
	return m.inner.IsTerminal(State(p))
}

func (m *EnvironmentTypedMachine) Name() string {
	return m.inner.Name()
}

func (m *EnvironmentTypedMachine) IsKnownState(p EnvironmentPhase) bool {
	return m.inner.IsKnownState(State(p))
}

func (m *EnvironmentTypedMachine) IsKnownEvent(e EnvironmentPhaseEvent) bool {
	return m.inner.IsKnownEvent(Event(e))
}

func (m *EnvironmentTypedMachine) MustParseState(s string) EnvironmentPhase {
	return EnvironmentPhase(m.inner.MustParseState(s))
}

func (m *EnvironmentTypedMachine) MustParseEvent(e string) EnvironmentPhaseEvent {
	return EnvironmentPhaseEvent(m.inner.MustParseEvent(e))
}

func (m *EnvironmentTypedMachine) ValidEvents(phase EnvironmentPhase) []EnvironmentPhaseEvent {
	untyped := m.inner.ValidEvents(State(phase))
	if untyped == nil {
		return nil
	}
	out := make([]EnvironmentPhaseEvent, len(untyped))
	for i, e := range untyped {
		out[i] = EnvironmentPhaseEvent(e)
	}
	return out
}
