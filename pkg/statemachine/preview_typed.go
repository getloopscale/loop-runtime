// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

// Phase 4 of ADR `2026-05-15-T6-1-typed-state-machines.md`:
// PreviewEnvironment typed wrapper.

// PreviewPhase is the typed state for the PreviewEnvironment lifecycle.
type PreviewPhase string

// PreviewPhaseEvent is the typed event for the PreviewEnvironment lifecycle.
type PreviewPhaseEvent string

const (
	PPhCreating      PreviewPhase = "Creating"
	PPhActive        PreviewPhase = "Active"
	PPhExpiring      PreviewPhase = "Expiring"
	PPhDeleted       PreviewPhase = "Deleted"
	PPhFailed        PreviewPhase = "Failed"
	PPhDeleting      PreviewPhase = "Deleting"
	PPhCleanupFailed PreviewPhase = "CleanupFailed"
)

const (
	PPEventCreationCompleted   PreviewPhaseEvent = "CreationCompleted"
	PPEventCreationFailed      PreviewPhaseEvent = "CreationFailed"
	PPEventExpirationTriggered PreviewPhaseEvent = "ExpirationTriggered"
	PPEventDeletionTriggered   PreviewPhaseEvent = "DeletionTriggered"
	PPEventCleanupCompleted    PreviewPhaseEvent = "CleanupCompleted"
	PPEventCleanupFailed       PreviewPhaseEvent = "CleanupFailed"
	PPEventGarbageCollected    PreviewPhaseEvent = "GarbageCollected"
)

// PreviewTypedMachine is the typed wrapper around the shared *Machine.
type PreviewTypedMachine struct {
	inner *Machine
}

func NewPreviewTypedMachine() *PreviewTypedMachine {
	return &PreviewTypedMachine{inner: NewPreviewMachine()}
}

func (m *PreviewTypedMachine) Transition(current PreviewPhase, event PreviewPhaseEvent) PreviewPhase {
	return PreviewPhase(m.inner.Transition(State(current), Event(event)))
}

func (m *PreviewTypedMachine) TryTransition(current PreviewPhase, event PreviewPhaseEvent) (PreviewPhase, error) {
	next, err := m.inner.TryTransition(State(current), Event(event))
	if err != nil {
		return "", err
	}
	return PreviewPhase(next), nil
}

func (m *PreviewTypedMachine) IsTerminal(p PreviewPhase) bool {
	return m.inner.IsTerminal(State(p))
}

func (m *PreviewTypedMachine) Name() string {
	return m.inner.Name()
}

func (m *PreviewTypedMachine) IsKnownState(p PreviewPhase) bool {
	return m.inner.IsKnownState(State(p))
}

func (m *PreviewTypedMachine) IsKnownEvent(e PreviewPhaseEvent) bool {
	return m.inner.IsKnownEvent(Event(e))
}

func (m *PreviewTypedMachine) MustParseState(s string) PreviewPhase {
	return PreviewPhase(m.inner.MustParseState(s))
}

func (m *PreviewTypedMachine) MustParseEvent(e string) PreviewPhaseEvent {
	return PreviewPhaseEvent(m.inner.MustParseEvent(e))
}

func (m *PreviewTypedMachine) ValidEvents(phase PreviewPhase) []PreviewPhaseEvent {
	untyped := m.inner.ValidEvents(State(phase))
	if untyped == nil {
		return nil
	}
	out := make([]PreviewPhaseEvent, len(untyped))
	for i, e := range untyped {
		out[i] = PreviewPhaseEvent(e)
	}
	return out
}
