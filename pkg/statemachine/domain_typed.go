// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

// Phase 3 of ADR `2026-05-15-T6-1-typed-state-machines.md`:
// Domain typed wrapper. Coexists with untyped NewDomainMachine().

// DomainPhase is the typed state for the Domain lifecycle.
// Supports BOTH v1 (TXT verification) and v2 (CF Zone Management)
// flows — see NewDomainMachine for the full state diagram.
type DomainPhase string

// DomainPhaseEvent is the typed event for the Domain lifecycle.
type DomainPhaseEvent string

const (
	// v1 states (TXT verification flow)
	DPhPendingVerification = DomainPhase(DomainPendingVerification)
	DPhVerified            = DomainPhase(DomainVerified)
	// v2 states (CF Zone flow)
	DPhPendingNS = DomainPhase(DomainPendingNS)
	DPhNSActive  = DomainPhase(DomainNSActive)
	// Shared states
	DPhProvisioning = DomainPhase(DomainProvisioning)
	DPhActive       = DomainPhase(DomainActive)
	DPhDegraded     = DomainPhase(DomainDegraded)
	DPhFailed       = DomainPhase(DomainFailed)
	DPhRemoving     = DomainPhase(DomainRemoving)
)

const (
	// v1 events
	DPEventVerify         = DomainPhaseEvent(DomainVerify)
	DPEventDegrade        = DomainPhaseEvent(DomainDegrade)
	DPEventCNAMELost      = DomainPhaseEvent(DomainCNAMELost)
	DPEventCNAMERecovered = DomainPhaseEvent(DomainCNAMERecovered)
	// v2 events
	DPEventNSActivated = DomainPhaseEvent(DomainNSActivated)
	DPEventNSTimeout   = DomainPhaseEvent(DomainNSTimeout)
	// Shared events
	DPEventProvision        = DomainPhaseEvent(DomainProvision)
	DPEventActivate         = DomainPhaseEvent(DomainActivate)
	DPEventProvisionFail    = DomainPhaseEvent(DomainProvisionFail)
	DPEventRetry            = DomainPhaseEvent(DomainRetry)
	DPEventRemove           = DomainPhaseEvent(DomainRemove)
	DPEventProvisionTimeout = DomainPhaseEvent(DomainProvisionTimeout)
)

// IsDomainPhaseTerminal returns true if the phase is terminal.
// Typed equivalent of package-level IsDomainTerminal — only "removing"
// is terminal.
func IsDomainPhaseTerminal(phase DomainPhase) bool {
	return IsDomainTerminal(State(phase))
}

// DomainTypedMachine is the typed DomainPhase machine.
type DomainTypedMachine = Typed[DomainPhase, DomainPhaseEvent]

// NewDomainTypedMachine returns the typed machine over NewDomainMachine(), which stays the
// single source of the transition table and the terminal set.
func NewDomainTypedMachine() *DomainTypedMachine {
	return newTyped[DomainPhase, DomainPhaseEvent](NewDomainMachine())
}
