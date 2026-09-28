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
	PBPhEmpty            = PlatformBootstrapPhase(PlatformBootstrapEmpty)
	PBPhPending          = PlatformBootstrapPhase(PlatformBootstrapPending)
	PBPhConfiguringGitea = PlatformBootstrapPhase(PlatformBootstrapConfiguringGitea)
	PBPhConfiguringArgo  = PlatformBootstrapPhase(PlatformBootstrapConfiguringArgo)
	PBPhWaitingForAPI    = PlatformBootstrapPhase(PlatformBootstrapWaitingForAPI)
	PBPhComplete         = PlatformBootstrapPhase(PlatformBootstrapComplete)
	PBPhFailed           = PlatformBootstrapPhase(PlatformBootstrapFailed)
)

const (
	PBPEventStart        = PlatformBootstrapPhaseEvent(PlatformBootstrapStart)
	PBPEventGiteaReady   = PlatformBootstrapPhaseEvent(PlatformBootstrapGiteaReady)
	PBPEventArgoCDReady  = PlatformBootstrapPhaseEvent(PlatformBootstrapArgoCDReady)
	PBPEventAPISeeded    = PlatformBootstrapPhaseEvent(PlatformBootstrapAPISeeded)
	PBPEventRetryExhaust = PlatformBootstrapPhaseEvent(PlatformBootstrapRetryExhaust)
)

// PlatformBootstrapTypedMachine is the typed PlatformBootstrapPhase machine.
type PlatformBootstrapTypedMachine = Typed[PlatformBootstrapPhase, PlatformBootstrapPhaseEvent]

// NewPlatformBootstrapTypedMachine returns the typed machine over NewPlatformBootstrapMachine(), which stays the
// single source of the transition table and the terminal set.
func NewPlatformBootstrapTypedMachine() *PlatformBootstrapTypedMachine {
	return newTyped[PlatformBootstrapPhase, PlatformBootstrapPhaseEvent](NewPlatformBootstrapMachine())
}
