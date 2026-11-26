// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

// Phase 3 of ADR `2026-05-15-T6-1-typed-state-machines.md`:
// Environment typed wrapper. Coexists with untyped NewEnvironmentMachine().

// EnvironmentPhase is the typed state for the Environment lifecycle.
type EnvironmentPhase string

// EnvironmentPhaseEvent is the typed event for the Environment lifecycle.
type EnvironmentPhaseEvent string

const (
	EPhEmpty    = EnvironmentPhase(EnvironmentEmpty)
	EPhPending  = EnvironmentPhase(EnvironmentPending)
	EPhActive   = EnvironmentPhase(EnvironmentActive)
	EPhFailed   = EnvironmentPhase(EnvironmentFailed)
	EPhDeleting = EnvironmentPhase(EnvironmentDeleting)
)

const (
	EPEventCreate          = EnvironmentPhaseEvent(EnvironmentCreate)
	EPEventProvisioned     = EnvironmentPhaseEvent(EnvironmentProvisioned)
	EPEventProvisionFailed = EnvironmentPhaseEvent(EnvironmentProvisionFailed)
	EPEventRetry           = EnvironmentPhaseEvent(EnvironmentRetry)
	EPEventDeleteRequested = EnvironmentPhaseEvent(EnvironmentDeleteRequested)
)

// EnvironmentTypedMachine is the typed EnvironmentPhase machine.
type EnvironmentTypedMachine = Typed[EnvironmentPhase, EnvironmentPhaseEvent]

// NewEnvironmentTypedMachine returns the typed machine over NewEnvironmentMachine(), which stays the
// single source of the transition table and the terminal set.
func NewEnvironmentTypedMachine() *EnvironmentTypedMachine {
	return newTyped[EnvironmentPhase, EnvironmentPhaseEvent](NewEnvironmentMachine())
}
