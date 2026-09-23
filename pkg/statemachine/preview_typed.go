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
	PPhEmpty         = PreviewPhase(PreviewEmpty)
	PPhCreating      = PreviewPhase(PreviewCreating)
	PPhActive        = PreviewPhase(PreviewActive)
	PPhExpiring      = PreviewPhase(PreviewExpiring)
	PPhDeleted       = PreviewPhase(PreviewDeleted)
	PPhFailed        = PreviewPhase(PreviewFailed)
	PPhDeleting      = PreviewPhase(PreviewDeleting)
	PPhCleanupFailed = PreviewPhase(PreviewCleanupFailed)
)

const (
	PPEventCreate              = PreviewPhaseEvent(PreviewCreate)
	PPEventCreationCompleted   = PreviewPhaseEvent(PreviewCreationCompleted)
	PPEventCreationFailed      = PreviewPhaseEvent(PreviewCreationFailed)
	PPEventExpirationTriggered = PreviewPhaseEvent(PreviewExpirationTriggered)
	PPEventDeletionTriggered   = PreviewPhaseEvent(PreviewDeletionTriggered)
	PPEventCleanupCompleted    = PreviewPhaseEvent(PreviewCleanupCompleted)
	PPEventCleanupFailed       = PreviewPhaseEvent(PreviewCleanupFailed_)
	PPEventGarbageCollected    = PreviewPhaseEvent(PreviewGarbageCollected)
)

// PreviewTypedMachine is the typed PreviewPhase machine.
type PreviewTypedMachine = Typed[PreviewPhase, PreviewPhaseEvent]

// NewPreviewTypedMachine returns the typed machine over NewPreviewMachine(), which stays the
// single source of the transition table and the terminal set.
func NewPreviewTypedMachine() *PreviewTypedMachine {
	return newTyped[PreviewPhase, PreviewPhaseEvent](NewPreviewMachine())
}
