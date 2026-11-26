// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

import "testing"

// TestDomainMachine_TransitionSequences exercises every valid DomainMachine
// transition chain. Phase 3 helper: collapses 5 near-clone tests (HappyPath,
// FailureAndRetry, CNAMELost/Recovered, ProvisionTimeout) into a single
// table-driven test that allows arbitrary starting state. Domain machine
// is unique among LoopZero state machines because flows begin at multiple
// states (PendingVerification for v1 flow, PendingNS for v2 CF Zone flow,
// Provisioning for retry tests, Active for CNAME-lost tests).
func TestDomainMachine_TransitionSequences(t *testing.T) {
	type step struct {
		event    Event
		expected State
	}
	cases := []struct {
		name  string
		from  State
		steps []step
	}{
		{
			name: "v1_HappyPath_PendingVerifyToRemoving",
			from: DomainPendingVerification,
			steps: []step{
				{DomainVerify, DomainVerified},
				{DomainProvision, DomainProvisioning},
				{DomainActivate, DomainActive},
				{DomainRemove, DomainRemoving},
			},
		},
		{
			name: "FailureAndRetry_ProvisioningToVerified",
			from: DomainProvisioning,
			steps: []step{
				{DomainProvisionFail, DomainFailed},
				{DomainRetry, DomainVerified},
			},
		},
		{
			name: "CNAMELost_GracePeriodElapsed",
			from: DomainActive,
			steps: []step{
				{DomainDegrade, DomainDegraded}, // CNAME lost, enters grace
				{DomainCNAMELost, DomainFailed}, // Grace expired
			},
		},
		{
			name: "CNAMERecovered_DuringGrace",
			from: DomainActive,
			steps: []step{
				{DomainDegrade, DomainDegraded},
				{DomainCNAMERecovered, DomainActive},
			},
		},
		{
			name:  "ProvisionTimeout",
			from:  DomainProvisioning,
			steps: []step{{DomainProvisionTimeout, DomainFailed}},
		},
		{
			name:  "AppRemoved_WhileActive",
			from:  DomainActive,
			steps: []step{{DomainAppRemoved, DomainFailed}},
		},
		{
			name: "AppRemoved_DuringGrace",
			from: DomainActive,
			steps: []step{
				{DomainDegrade, DomainDegraded},
				{DomainAppRemoved, DomainFailed},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := NewDomainMachine()
			state := c.from
			for _, s := range c.steps {
				state = m.Transition(state, s.event)
				if state != s.expected {
					t.Fatalf("after event %q from initial %q: expected %s, got %s",
						s.event, c.from, s.expected, state)
				}
			}
		})
	}
}

func TestDomainMachine_InvalidTransition_Panics(t *testing.T) {
	m := NewDomainMachine()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on invalid transition (active → verified), got nil")
		}
	}()

	// active → DomainVerify is not a valid transition
	m.Transition(DomainActive, DomainVerify)
}

func TestDomainMachine_RemovingIsTerminal(t *testing.T) {
	if !IsDomainTerminal(DomainRemoving) {
		t.Error("expected DomainRemoving to be terminal")
	}
	if IsDomainTerminal(DomainActive) {
		t.Error("expected DomainActive to NOT be terminal")
	}
	if IsDomainTerminal(DomainDegraded) {
		t.Error("expected DomainDegraded to NOT be terminal")
	}
}

func TestDomainMachine_AllStatesCanRemove(t *testing.T) {
	m := NewDomainMachine()

	removable := []State{
		DomainPendingVerification,
		DomainVerified,
		DomainPendingNS,
		DomainNSActive,
		DomainProvisioning,
		DomainActive,
		DomainDegraded,
		DomainFailed,
	}

	for _, s := range removable {
		state := m.Transition(s, DomainRemove)
		if state != DomainRemoving {
			t.Errorf("expected %s + DomainRemove → removing, got %s", s, state)
		}
	}
}

// --- v2: CF Zone flow tests ---

func TestDomainMachine_V2_HappyPath(t *testing.T) {
	m := NewDomainMachine()

	state := DomainPendingNS
	state = m.Transition(state, DomainNSActivated)
	if state != DomainNSActive {
		t.Fatalf("expected ns_active, got %s", state)
	}
	state = m.Transition(state, DomainProvision)
	if state != DomainProvisioning {
		t.Fatalf("expected provisioning, got %s", state)
	}
	state = m.Transition(state, DomainActivate)
	if state != DomainActive {
		t.Fatalf("expected active, got %s", state)
	}
}

func TestDomainMachine_V2_NSTimeout(t *testing.T) {
	m := NewDomainMachine()

	state := m.Transition(DomainPendingNS, DomainNSTimeout)
	if state != DomainFailed {
		t.Fatalf("expected failed after NS timeout, got %s", state)
	}
}

func TestDomainMachine_V2_FailedRecovery(t *testing.T) {
	m := NewDomainMachine()

	// NS timeout → failed
	state := m.Transition(DomainPendingNS, DomainNSTimeout)
	if state != DomainFailed {
		t.Fatalf("expected failed, got %s", state)
	}

	// NS comes back → ns_active (retry)
	state = m.Transition(state, DomainNSActivated)
	if state != DomainNSActive {
		t.Fatalf("expected ns_active after NS recovery, got %s", state)
	}
}
