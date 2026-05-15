// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

// Phase 4 of ADR `2026-05-15-T6-1-typed-state-machines.md`:
// PlatformBootstrap typed wrapper — final machine of the 11.
// After this iter, all 11 state machines have typed wrappers.

// PlatformBootstrapPhase is the typed state for the PlatformBootstrap lifecycle.
type PlatformBootstrapPhase string

// PlatformBootstrapPhaseEvent is the typed event for the PlatformBootstrap lifecycle.
type PlatformBootstrapPhaseEvent string

const (
	PBPhEmpty            PlatformBootstrapPhase = ""
	PBPhPending          PlatformBootstrapPhase = "Pending"
	PBPhConfiguringGitea PlatformBootstrapPhase = "ConfiguringGitea"
	PBPhConfiguringArgo  PlatformBootstrapPhase = "ConfiguringArgoCD"
	PBPhWaitingForAPI    PlatformBootstrapPhase = "WaitingForAPI"
	PBPhComplete         PlatformBootstrapPhase = "BootstrapComplete"
	PBPhFailed           PlatformBootstrapPhase = "Failed"
)

const (
	PBPEventStart        PlatformBootstrapPhaseEvent = "Start"
	PBPEventGiteaReady   PlatformBootstrapPhaseEvent = "GiteaReady"
	PBPEventArgoCDReady  PlatformBootstrapPhaseEvent = "ArgoCDReady"
	PBPEventAPISeeded    PlatformBootstrapPhaseEvent = "APISeeded"
	PBPEventRetryExhaust PlatformBootstrapPhaseEvent = "RetryExhausted"
)

// PlatformBootstrapTypedMachine is the typed wrapper around the shared *Machine.
type PlatformBootstrapTypedMachine struct {
	inner *Machine
}

func NewPlatformBootstrapTypedMachine() *PlatformBootstrapTypedMachine {
	return &PlatformBootstrapTypedMachine{inner: NewPlatformBootstrapMachine()}
}

func (m *PlatformBootstrapTypedMachine) Transition(current PlatformBootstrapPhase, event PlatformBootstrapPhaseEvent) PlatformBootstrapPhase {
	return PlatformBootstrapPhase(m.inner.Transition(State(current), Event(event)))
}

func (m *PlatformBootstrapTypedMachine) TryTransition(current PlatformBootstrapPhase, event PlatformBootstrapPhaseEvent) (PlatformBootstrapPhase, error) {
	next, err := m.inner.TryTransition(State(current), Event(event))
	if err != nil {
		return "", err
	}
	return PlatformBootstrapPhase(next), nil
}

func (m *PlatformBootstrapTypedMachine) IsTerminal(p PlatformBootstrapPhase) bool {
	return m.inner.IsTerminal(State(p))
}

func (m *PlatformBootstrapTypedMachine) Name() string {
	return m.inner.Name()
}

func (m *PlatformBootstrapTypedMachine) IsKnownState(p PlatformBootstrapPhase) bool {
	return m.inner.IsKnownState(State(p))
}

func (m *PlatformBootstrapTypedMachine) IsKnownEvent(e PlatformBootstrapPhaseEvent) bool {
	return m.inner.IsKnownEvent(Event(e))
}

func (m *PlatformBootstrapTypedMachine) MustParseState(s string) PlatformBootstrapPhase {
	return PlatformBootstrapPhase(m.inner.MustParseState(s))
}

func (m *PlatformBootstrapTypedMachine) MustParseEvent(e string) PlatformBootstrapPhaseEvent {
	return PlatformBootstrapPhaseEvent(m.inner.MustParseEvent(e))
}

func (m *PlatformBootstrapTypedMachine) ValidEvents(phase PlatformBootstrapPhase) []PlatformBootstrapPhaseEvent {
	untyped := m.inner.ValidEvents(State(phase))
	if untyped == nil {
		return nil
	}
	out := make([]PlatformBootstrapPhaseEvent, len(untyped))
	for i, e := range untyped {
		out[i] = PlatformBootstrapPhaseEvent(e)
	}
	return out
}
