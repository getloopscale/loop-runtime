// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

// Phase 4 of ADR `2026-05-15-T6-1-typed-state-machines.md`:
// Promotion typed wrapper.

// PromotionPhase is the typed state for the Promotion lifecycle.
type PromotionPhase string

// PromotionPhaseEvent is the typed event for the Promotion lifecycle.
type PromotionPhaseEvent string

const (
	PrPhValidating   = PromotionPhase(PromotionValidating)
	PrPhCommitting   = PromotionPhase(PromotionCommitting)
	PrPhSyncing      = PromotionPhase(PromotionSyncing)
	PrPhCompleted    = PromotionPhase(PromotionCompleted)
	PrPhRejected     = PromotionPhase(PromotionRejected)
	PrPhCommitFailed = PromotionPhase(PromotionCommitFailed)
	PrPhSyncFailed   = PromotionPhase(PromotionSyncFailed)
)

const (
	PrPEventValidationPassed = PromotionPhaseEvent(PromotionValidationPassed)
	PrPEventValidationFailed = PromotionPhaseEvent(PromotionValidationFailed)
	PrPEventCommitCompleted  = PromotionPhaseEvent(PromotionCommitCompleted)
	PrPEventCommitFailed     = PromotionPhaseEvent(PromotionCommitFailed_)
	PrPEventSyncCompleted    = PromotionPhaseEvent(PromotionSyncCompleted)
	PrPEventSyncFailed       = PromotionPhaseEvent(PromotionSyncFailed_)
)

// PromotionTypedMachine is the typed PromotionPhase machine.
type PromotionTypedMachine = Typed[PromotionPhase, PromotionPhaseEvent]

// NewPromotionTypedMachine returns the typed machine over NewPromotionMachine(), which stays the
// single source of the transition table and the terminal set.
func NewPromotionTypedMachine() *PromotionTypedMachine {
	return newTyped[PromotionPhase, PromotionPhaseEvent](NewPromotionMachine())
}
