// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

// Phase 4 of ADR `2026-05-15-T6-1-typed-state-machines.md`:
// Promotion typed wrapper.

// PromotionPhase is the typed state for the Promotion lifecycle.
type PromotionPhase string

// PromotionPhaseEvent is the typed event for the Promotion lifecycle.
type PromotionPhaseEvent string

const (
	PrPhValidating   PromotionPhase = "VALIDATING"
	PrPhCommitting   PromotionPhase = "COMMITTING"
	PrPhSyncing      PromotionPhase = "SYNCING"
	PrPhCompleted    PromotionPhase = "COMPLETED"
	PrPhRejected     PromotionPhase = "REJECTED"
	PrPhCommitFailed PromotionPhase = "COMMIT_FAILED"
	PrPhSyncFailed   PromotionPhase = "SYNC_FAILED"
)

const (
	PrPEventValidationPassed PromotionPhaseEvent = "ValidationPassed"
	PrPEventValidationFailed PromotionPhaseEvent = "ValidationFailed"
	PrPEventCommitCompleted  PromotionPhaseEvent = "CommitCompleted"
	PrPEventCommitFailed     PromotionPhaseEvent = "CommitFailed"
	PrPEventSyncCompleted    PromotionPhaseEvent = "SyncCompleted"
	PrPEventSyncFailed       PromotionPhaseEvent = "SyncFailed"
)

// PromotionTypedMachine is the typed wrapper around the shared *Machine.
type PromotionTypedMachine struct {
	inner *Machine
}

func NewPromotionTypedMachine() *PromotionTypedMachine {
	return &PromotionTypedMachine{inner: NewPromotionMachine()}
}

func (m *PromotionTypedMachine) Transition(current PromotionPhase, event PromotionPhaseEvent) PromotionPhase {
	return PromotionPhase(m.inner.Transition(State(current), Event(event)))
}

func (m *PromotionTypedMachine) TryTransition(current PromotionPhase, event PromotionPhaseEvent) (PromotionPhase, error) {
	next, err := m.inner.TryTransition(State(current), Event(event))
	if err != nil {
		return "", err
	}
	return PromotionPhase(next), nil
}

func (m *PromotionTypedMachine) IsTerminal(p PromotionPhase) bool {
	return m.inner.IsTerminal(State(p))
}

func (m *PromotionTypedMachine) Name() string {
	return m.inner.Name()
}

func (m *PromotionTypedMachine) IsKnownState(p PromotionPhase) bool {
	return m.inner.IsKnownState(State(p))
}

func (m *PromotionTypedMachine) IsKnownEvent(e PromotionPhaseEvent) bool {
	return m.inner.IsKnownEvent(Event(e))
}

func (m *PromotionTypedMachine) MustParseState(s string) PromotionPhase {
	return PromotionPhase(m.inner.MustParseState(s))
}

func (m *PromotionTypedMachine) MustParseEvent(e string) PromotionPhaseEvent {
	return PromotionPhaseEvent(m.inner.MustParseEvent(e))
}

func (m *PromotionTypedMachine) ValidEvents(phase PromotionPhase) []PromotionPhaseEvent {
	untyped := m.inner.ValidEvents(State(phase))
	if untyped == nil {
		return nil
	}
	out := make([]PromotionPhaseEvent, len(untyped))
	for i, e := range untyped {
		out[i] = PromotionPhaseEvent(e)
	}
	return out
}
