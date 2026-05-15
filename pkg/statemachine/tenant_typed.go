// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

// Phase 3 of ADR `2026-05-15-T6-1-typed-state-machines.md` (sponsor-
// ratified 2026-05-16, Phase 1+2 shipped iters 31+33):
//
// Tenant typed wrapper. Coexists with untyped NewTenantMachine().

// TenantPhase is the typed state for the Tenant lifecycle.
type TenantPhase string

// TenantPhaseEvent is the typed event for the Tenant lifecycle.
type TenantPhaseEvent string

const (
	TPhEmpty            TenantPhase = ""
	TPhProvisioning     TenantPhase = "Provisioning"
	TPhActive           TenantPhase = "Active"
	TPhSuspended        TenantPhase = "Suspended"
	TPhPendingDeletion  TenantPhase = "PendingDeletion"
	TPhDeleting         TenantPhase = "Deleting"
)

const (
	TPEventCreate          TenantPhaseEvent = "Create"
	TPEventAllReady        TenantPhaseEvent = "AllConditionsReady"
	TPEventSpecChanged     TenantPhaseEvent = "SpecChanged"
	TPEventSuspend         TenantPhaseEvent = "Suspend"
	TPEventUnsuspend       TenantPhaseEvent = "Unsuspend"
	TPEventRequestDeletion TenantPhaseEvent = "RequestDeletion"
	TPEventGraceExpired    TenantPhaseEvent = "GraceExpired"
	TPEventRestore         TenantPhaseEvent = "Restore"
	TPEventDelete          TenantPhaseEvent = "Delete"
)

// TenantTypedMachine is the typed wrapper around the shared *Machine.
type TenantTypedMachine struct {
	inner *Machine
}

// NewTenantTypedMachine constructs the typed Tenant state machine.
func NewTenantTypedMachine() *TenantTypedMachine {
	return &TenantTypedMachine{inner: NewTenantMachine()}
}

func (m *TenantTypedMachine) Transition(current TenantPhase, event TenantPhaseEvent) TenantPhase {
	return TenantPhase(m.inner.Transition(State(current), Event(event)))
}

func (m *TenantTypedMachine) TryTransition(current TenantPhase, event TenantPhaseEvent) (TenantPhase, error) {
	next, err := m.inner.TryTransition(State(current), Event(event))
	if err != nil {
		return "", err
	}
	return TenantPhase(next), nil
}

func (m *TenantTypedMachine) IsTerminal(p TenantPhase) bool {
	return m.inner.IsTerminal(State(p))
}

func (m *TenantTypedMachine) Name() string {
	return m.inner.Name()
}

func (m *TenantTypedMachine) IsKnownState(p TenantPhase) bool {
	return m.inner.IsKnownState(State(p))
}

func (m *TenantTypedMachine) IsKnownEvent(e TenantPhaseEvent) bool {
	return m.inner.IsKnownEvent(Event(e))
}

func (m *TenantTypedMachine) MustParseState(s string) TenantPhase {
	return TenantPhase(m.inner.MustParseState(s))
}

func (m *TenantTypedMachine) MustParseEvent(e string) TenantPhaseEvent {
	return TenantPhaseEvent(m.inner.MustParseEvent(e))
}

func (m *TenantTypedMachine) ValidEvents(phase TenantPhase) []TenantPhaseEvent {
	untyped := m.inner.ValidEvents(State(phase))
	if untyped == nil {
		return nil
	}
	out := make([]TenantPhaseEvent, len(untyped))
	for i, e := range untyped {
		out[i] = TenantPhaseEvent(e)
	}
	return out
}
