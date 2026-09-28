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
	APhEmpty       = ApplicationPhase(ApplicationEmpty)
	APhProgressing = ApplicationPhase(ApplicationProgressing)
	APhServing     = ApplicationPhase(ApplicationServing)
	APhReachable   = ApplicationPhase(ApplicationReachable)
	APhDegraded    = ApplicationPhase(ApplicationDegraded)
	APhDown        = ApplicationPhase(ApplicationDown)
	APhCleaned     = ApplicationPhase(ApplicationCleaned)
)

const (
	APEventCreate         = ApplicationPhaseEvent(ApplicationCreate)
	APEventEndpointsReady = ApplicationPhaseEvent(ApplicationEndpointsReady)
	APEventEndpointsLost  = ApplicationPhaseEvent(ApplicationEndpointsLost)
	APEventRouteOK        = ApplicationPhaseEvent(ApplicationRouteOK)
	APEventRouteFail      = ApplicationPhaseEvent(ApplicationRouteFail)
	APEventUpdate         = ApplicationPhaseEvent(ApplicationUpdate)
	APEventOrphaned       = ApplicationPhaseEvent(ApplicationOrphaned)
)

// ApplicationTypedMachine is the typed ApplicationPhase machine.
type ApplicationTypedMachine = Typed[ApplicationPhase, ApplicationPhaseEvent]

// NewApplicationTypedMachine returns the typed machine over NewApplicationMachine(), which stays the
// single source of the transition table and the terminal set.
func NewApplicationTypedMachine() *ApplicationTypedMachine {
	return newTyped[ApplicationPhase, ApplicationPhaseEvent](NewApplicationMachine())
}
