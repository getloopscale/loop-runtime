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
	DPhPendingVerification DomainPhase = "pending_verification"
	DPhVerified            DomainPhase = "verified"
	// v2 states (CF Zone flow)
	DPhPendingNS DomainPhase = "pending_ns"
	DPhNSActive  DomainPhase = "ns_active"
	// Shared states
	DPhProvisioning DomainPhase = "provisioning"
	DPhActive       DomainPhase = "active"
	DPhDegraded     DomainPhase = "degraded"
	DPhFailed       DomainPhase = "failed"
	DPhRemoving     DomainPhase = "removing"
)

const (
	// v1 events
	DPEventVerify         DomainPhaseEvent = "DomainVerify"
	DPEventDegrade        DomainPhaseEvent = "DomainDegrade"
	DPEventCNAMELost      DomainPhaseEvent = "DomainCNAMELost"
	DPEventCNAMERecovered DomainPhaseEvent = "DomainCNAMERecovered"
	// v2 events
	DPEventNSActivated DomainPhaseEvent = "DomainNSActivated"
	DPEventNSTimeout   DomainPhaseEvent = "DomainNSTimeout"
	// Shared events
	DPEventProvision        DomainPhaseEvent = "DomainProvision"
	DPEventActivate         DomainPhaseEvent = "DomainActivate"
	DPEventProvisionFail    DomainPhaseEvent = "DomainProvisionFail"
	DPEventRetry            DomainPhaseEvent = "DomainRetry"
	DPEventRemove           DomainPhaseEvent = "DomainRemove"
	DPEventProvisionTimeout DomainPhaseEvent = "DomainProvisionTimeout"
)

// DomainTypedMachine is the typed wrapper around the shared *Machine.
type DomainTypedMachine struct {
	inner *Machine
}

func NewDomainTypedMachine() *DomainTypedMachine {
	return &DomainTypedMachine{inner: NewDomainMachine()}
}

func (m *DomainTypedMachine) Transition(current DomainPhase, event DomainPhaseEvent) DomainPhase {
	return DomainPhase(m.inner.Transition(State(current), Event(event)))
}

func (m *DomainTypedMachine) TryTransition(current DomainPhase, event DomainPhaseEvent) (DomainPhase, error) {
	next, err := m.inner.TryTransition(State(current), Event(event))
	if err != nil {
		return "", err
	}
	return DomainPhase(next), nil
}

func (m *DomainTypedMachine) IsTerminal(p DomainPhase) bool {
	return m.inner.IsTerminal(State(p))
}

func (m *DomainTypedMachine) Name() string {
	return m.inner.Name()
}

func (m *DomainTypedMachine) IsKnownState(p DomainPhase) bool {
	return m.inner.IsKnownState(State(p))
}

func (m *DomainTypedMachine) IsKnownEvent(e DomainPhaseEvent) bool {
	return m.inner.IsKnownEvent(Event(e))
}

func (m *DomainTypedMachine) MustParseState(s string) DomainPhase {
	return DomainPhase(m.inner.MustParseState(s))
}

func (m *DomainTypedMachine) MustParseEvent(e string) DomainPhaseEvent {
	return DomainPhaseEvent(m.inner.MustParseEvent(e))
}

func (m *DomainTypedMachine) ValidEvents(phase DomainPhase) []DomainPhaseEvent {
	untyped := m.inner.ValidEvents(State(phase))
	if untyped == nil {
		return nil
	}
	out := make([]DomainPhaseEvent, len(untyped))
	for i, e := range untyped {
		out[i] = DomainPhaseEvent(e)
	}
	return out
}

// IsDomainPhaseTerminal returns true if the phase is terminal.
// Typed equivalent of package-level IsDomainTerminal — only "removing"
// is terminal.
func IsDomainPhaseTerminal(phase DomainPhase) bool {
	return phase == DPhRemoving
}
