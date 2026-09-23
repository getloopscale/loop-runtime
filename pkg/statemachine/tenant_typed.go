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
	TPhEmpty           = TenantPhase(TenantEmpty)
	TPhProvisioning    = TenantPhase(TenantProvisioning)
	TPhActive          = TenantPhase(TenantActive)
	TPhSuspended       = TenantPhase(TenantSuspended)
	TPhPendingDeletion = TenantPhase(TenantPendingDeletion)
	TPhDeleting        = TenantPhase(TenantDeleting)
)

const (
	TPEventCreate          = TenantPhaseEvent(TenantCreate)
	TPEventAllReady        = TenantPhaseEvent(TenantAllReady)
	TPEventSpecChanged     = TenantPhaseEvent(TenantSpecChanged)
	TPEventSuspend         = TenantPhaseEvent(TenantSuspend)
	TPEventUnsuspend       = TenantPhaseEvent(TenantUnsuspend)
	TPEventRequestDeletion = TenantPhaseEvent(TenantRequestDeletion)
	TPEventGraceExpired    = TenantPhaseEvent(TenantGraceExpired)
	TPEventRestore         = TenantPhaseEvent(TenantRestore)
	TPEventDelete          = TenantPhaseEvent(TenantDelete)
)

// TenantTypedMachine is the typed TenantPhase machine.
type TenantTypedMachine = Typed[TenantPhase, TenantPhaseEvent]

// NewTenantTypedMachine returns the typed machine over NewTenantMachine(), which stays the
// single source of the transition table and the terminal set.
func NewTenantTypedMachine() *TenantTypedMachine {
	return newTyped[TenantPhase, TenantPhaseEvent](NewTenantMachine())
}
