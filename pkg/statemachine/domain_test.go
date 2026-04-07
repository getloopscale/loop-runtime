// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statemachine

import "testing"

func TestDomainMachine_HappyPath(t *testing.T) {
	m := NewDomainMachine()

	state := DomainPendingVerification
	state = m.Transition(state, DomainVerify)
	if state != DomainVerified {
		t.Fatalf("expected verified, got %s", state)
	}
	state = m.Transition(state, DomainProvision)
	if state != DomainProvisioning {
		t.Fatalf("expected provisioning, got %s", state)
	}
	state = m.Transition(state, DomainActivate)
	if state != DomainActive {
		t.Fatalf("expected active, got %s", state)
	}
	state = m.Transition(state, DomainRemove)
	if state != DomainRemoving {
		t.Fatalf("expected removing, got %s", state)
	}
}

func TestDomainMachine_FailureAndRetry(t *testing.T) {
	m := NewDomainMachine()

	state := m.Transition(DomainProvisioning, DomainProvisionFail)
	if state != DomainFailed {
		t.Fatalf("expected failed, got %s", state)
	}
	state = m.Transition(state, DomainRetry)
	if state != DomainVerified {
		t.Fatalf("expected verified after retry, got %s", state)
	}
}

func TestDomainMachine_CNAMELost_TransitionsToRemoving(t *testing.T) {
	m := NewDomainMachine()

	state := m.Transition(DomainActive, DomainCNAMELost)
	if state != DomainRemoving {
		t.Fatalf("expected removing after CNAME lost, got %s", state)
	}
}

func TestDomainMachine_ProvisionTimeout(t *testing.T) {
	m := NewDomainMachine()

	state := m.Transition(DomainProvisioning, DomainProvisionTimeout)
	if state != DomainFailed {
		t.Fatalf("expected failed after timeout, got %s", state)
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
}

func TestDomainMachine_AllStatesCanRemove(t *testing.T) {
	m := NewDomainMachine()

	removable := []State{
		DomainPendingVerification,
		DomainVerified,
		DomainProvisioning,
		DomainActive,
		DomainFailed,
	}

	for _, s := range removable {
		state := m.Transition(s, DomainRemove)
		if state != DomainRemoving {
			t.Errorf("expected %s + DomainRemove → removing, got %s", s, state)
		}
	}
}
