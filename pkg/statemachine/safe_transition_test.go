// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

import (
	"errors"
	"testing"
)

// Tests for SafeTransition methods across all 11 typed wrappers
// (Phase 5 prep work for ADR 2026-05-15-T6-1-typed-state-machines).

func TestSafeTransition_ExposedRoute_HappyAndInvalid(t *testing.T) {
	m := NewExposedRouteTypedMachine()
	next, err := m.SafeTransition(XRStateEmpty, XREventCreate)
	if err != nil || next != XRStatePending {
		t.Errorf("happy path: got %q err=%v, want Pending nil", next, err)
	}
	// Invalid: ProbeOK is not valid from Empty
	_, err = m.SafeTransition(XRStateEmpty, XREventProbeOK)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("invalid transition: want ErrInvalidTransition, got %v", err)
	}
}

func TestSafeTransition_Build_HappyAndInvalid(t *testing.T) {
	m := NewBuildTypedMachine()
	next, err := m.SafeTransition(BPhQueued, BPEventStarted)
	if err != nil || next != BPhRunning {
		t.Errorf("happy path: got %q err=%v, want RUNNING nil", next, err)
	}
	// Invalid: BuildStarted from terminal SUCCEEDED
	_, err = m.SafeTransition(BPhSucceeded, BPEventStarted)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("terminal source: want ErrInvalidTransition, got %v", err)
	}
}

func TestSafeTransition_Release_HappyAndInvalid(t *testing.T) {
	m := NewReleaseTypedMachine()
	next, err := m.SafeTransition(RPhReady, RPEventGitMirrorCompleted)
	if err != nil || next != RPhPending {
		t.Errorf("happy path: got %q err=%v, want PENDING nil", next, err)
	}
	_, err = m.SafeTransition(RPhCancelled, RPEventReactivated)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("terminal source: want ErrInvalidTransition, got %v", err)
	}
}

func TestSafeTransition_Tenant_HappyAndInvalid(t *testing.T) {
	m := NewTenantTypedMachine()
	next, err := m.SafeTransition(TPhEmpty, TPEventCreate)
	if err != nil || next != TPhProvisioning {
		t.Errorf("happy path: got %q err=%v, want Provisioning nil", next, err)
	}
	_, err = m.SafeTransition(TPhDeleting, TPEventRestore)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("terminal source: want ErrInvalidTransition, got %v", err)
	}
}

func TestSafeTransition_Environment_HappyAndInvalid(t *testing.T) {
	m := NewEnvironmentTypedMachine()
	next, err := m.SafeTransition(EPhEmpty, EPEventCreate)
	if err != nil || next != EPhPending {
		t.Errorf("happy path: got %q err=%v, want Pending nil", next, err)
	}
	_, err = m.SafeTransition(EPhDeleting, EPEventCreate)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("terminal source: want ErrInvalidTransition, got %v", err)
	}
}

func TestSafeTransition_Application_HappyAndInvalid(t *testing.T) {
	m := NewApplicationTypedMachine()
	next, err := m.SafeTransition(APhEmpty, APEventCreate)
	if err != nil || next != APhProgressing {
		t.Errorf("happy path: got %q err=%v, want Progressing nil", next, err)
	}
	_, err = m.SafeTransition(APhCleaned, APEventCreate)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("terminal source: want ErrInvalidTransition, got %v", err)
	}
}

func TestSafeTransition_Domain_HappyAndInvalid(t *testing.T) {
	m := NewDomainTypedMachine()
	next, err := m.SafeTransition(DPhPendingVerification, DPEventVerify)
	if err != nil || next != DPhVerified {
		t.Errorf("happy path: got %q err=%v, want verified nil", next, err)
	}
	_, err = m.SafeTransition(DPhRemoving, DPEventVerify)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("terminal source: want ErrInvalidTransition, got %v", err)
	}
}

func TestSafeTransition_Preview_HappyAndInvalid(t *testing.T) {
	m := NewPreviewTypedMachine()
	next, err := m.SafeTransition(PPhCreating, PPEventCreationCompleted)
	if err != nil || next != PPhActive {
		t.Errorf("happy path: got %q err=%v, want Active nil", next, err)
	}
	_, err = m.SafeTransition(PPhDeleted, PPEventCreationCompleted)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("terminal source: want ErrInvalidTransition, got %v", err)
	}
}

func TestSafeTransition_Promotion_HappyAndInvalid(t *testing.T) {
	m := NewPromotionTypedMachine()
	next, err := m.SafeTransition(PrPhValidating, PrPEventValidationPassed)
	if err != nil || next != PrPhCommitting {
		t.Errorf("happy path: got %q err=%v, want COMMITTING nil", next, err)
	}
	_, err = m.SafeTransition(PrPhCompleted, PrPEventValidationPassed)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("terminal source: want ErrInvalidTransition, got %v", err)
	}
}

func TestSafeTransition_Rollback_HappyAndInvalid(t *testing.T) {
	m := NewRollbackTypedMachine()
	next, err := m.SafeTransition(RbPhValidating, RbPEventValidationPassed)
	if err != nil || next != RbPhReverting {
		t.Errorf("happy path: got %q err=%v, want REVERTING nil", next, err)
	}
	_, err = m.SafeTransition(RbPhCompleted, RbPEventValidationPassed)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("terminal source: want ErrInvalidTransition, got %v", err)
	}
}

func TestSafeTransition_PlatformBootstrap_HappyAndInvalid(t *testing.T) {
	m := NewPlatformBootstrapTypedMachine()
	next, err := m.SafeTransition(PBPhEmpty, PBPEventStart)
	if err != nil || next != PBPhPending {
		t.Errorf("happy path: got %q err=%v, want Pending nil", next, err)
	}
	_, err = m.SafeTransition(PBPhComplete, PBPEventStart)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("terminal source: want ErrInvalidTransition, got %v", err)
	}
}

func TestSafeTransition_ErrSentinel_DistinctIdentity(t *testing.T) {
	// errors.Is(ErrInvalidTransition, ErrInvalidTransition) must hold
	// — paranoid check that the sentinel comparison works as a contract.
	if !errors.Is(ErrInvalidTransition, ErrInvalidTransition) {
		t.Fatal("ErrInvalidTransition is not errors.Is-equal to itself")
	}
}
