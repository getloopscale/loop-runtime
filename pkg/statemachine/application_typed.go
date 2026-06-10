// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

// Phase 3 of ADR `2026-05-15-T6-1-typed-state-machines.md`:
// LoopZeroApplication typed wrapper. Coexists with untyped
// NewApplicationMachine().

// ApplicationPhase is the typed state for the LoopZeroApplication
// lifecycle. The machine is CYCLIC (Reachable↔Degraded) — only
// Cleaned is terminal (reaper-driven orphan cleanup).
type ApplicationPhase string

// ApplicationPhaseEvent is the typed event for the LoopZeroApplication lifecycle.
type ApplicationPhaseEvent string

const (
	APhEmpty       ApplicationPhase = ""
	APhProgressing ApplicationPhase = "Progressing"
	APhServing     ApplicationPhase = "Serving"
	APhReachable   ApplicationPhase = "Reachable"
	APhDegraded    ApplicationPhase = "Degraded"
	APhDown        ApplicationPhase = "Down"
	APhCleaned     ApplicationPhase = "Cleaned"
)

const (
	APEventCreate         ApplicationPhaseEvent = "Create"
	APEventEndpointsReady ApplicationPhaseEvent = "EndpointsReady"
	APEventEndpointsLost  ApplicationPhaseEvent = "EndpointsLost"
	APEventRouteOK        ApplicationPhaseEvent = "RouteOK"
	APEventRouteFail      ApplicationPhaseEvent = "RouteFail"
	APEventUpdate         ApplicationPhaseEvent = "Update"
	APEventOrphaned       ApplicationPhaseEvent = "Orphaned"
)

// ApplicationTypedMachine is the typed wrapper around the shared *Machine.
type ApplicationTypedMachine struct {
	inner *Machine
}

func NewApplicationTypedMachine() *ApplicationTypedMachine {
	return &ApplicationTypedMachine{inner: NewApplicationMachine()}
}

func (m *ApplicationTypedMachine) Transition(current ApplicationPhase, event ApplicationPhaseEvent) ApplicationPhase {
	return ApplicationPhase(m.inner.Transition(State(current), Event(event)))
}

func (m *ApplicationTypedMachine) TryTransition(current ApplicationPhase, event ApplicationPhaseEvent) (ApplicationPhase, error) {
	next, err := m.inner.TryTransition(State(current), Event(event))
	if err != nil {
		return "", err
	}
	return ApplicationPhase(next), nil
}

func (m *ApplicationTypedMachine) IsTerminal(p ApplicationPhase) bool {
	return m.inner.IsTerminal(State(p))
}

func (m *ApplicationTypedMachine) Name() string {
	return m.inner.Name()
}

func (m *ApplicationTypedMachine) IsKnownState(p ApplicationPhase) bool {
	return m.inner.IsKnownState(State(p))
}

func (m *ApplicationTypedMachine) IsKnownEvent(e ApplicationPhaseEvent) bool {
	return m.inner.IsKnownEvent(Event(e))
}

func (m *ApplicationTypedMachine) MustParseState(s string) ApplicationPhase {
	return ApplicationPhase(m.inner.MustParseState(s))
}

func (m *ApplicationTypedMachine) MustParseEvent(e string) ApplicationPhaseEvent {
	return ApplicationPhaseEvent(m.inner.MustParseEvent(e))
}

func (m *ApplicationTypedMachine) ValidEvents(phase ApplicationPhase) []ApplicationPhaseEvent {
	untyped := m.inner.ValidEvents(State(phase))
	if untyped == nil {
		return nil
	}
	out := make([]ApplicationPhaseEvent, len(untyped))
	for i, e := range untyped {
		out[i] = ApplicationPhaseEvent(e)
	}
	return out
}
