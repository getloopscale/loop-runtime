// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

import "testing"

// Phase 3 tests for ADR `2026-05-15-T6-1-typed-state-machines.md`.
// Covers Tenant + Environment + Application + Domain typed wrappers.
// Single file (vs 4 separate test files) because each machine's typed
// surface follows the identical pattern from Phase 1+2.

// =============================================================================
// Tenant
// =============================================================================

func TestTenantTypedMachine_LifecycleHappyPath(t *testing.T) {
	m := NewTenantTypedMachine()
	next := m.Transition(TPhEmpty, TPEventCreate)
	if next != TPhProvisioning {
		t.Fatalf("Create from Empty: want Provisioning, got %q", next)
	}
	next = m.Transition(TPhProvisioning, TPEventAllReady)
	if next != TPhActive {
		t.Fatalf("AllReady from Provisioning: want Active, got %q", next)
	}
}

func TestTenantTypedMachine_SoftDeleteFlow(t *testing.T) {
	m := NewTenantTypedMachine()
	// MEET-052 soft delete: Active → PendingDeletion → Deleting (or Active via Restore)
	next := m.Transition(TPhActive, TPEventRequestDeletion)
	if next != TPhPendingDeletion {
		t.Fatalf("RequestDeletion: want PendingDeletion, got %q", next)
	}
	// Restore cancels deletion
	next = m.Transition(TPhPendingDeletion, TPEventRestore)
	if next != TPhActive {
		t.Fatalf("Restore: want Active, got %q", next)
	}
	// GraceExpired progresses to Deleting
	next = m.Transition(TPhPendingDeletion, TPEventGraceExpired)
	if next != TPhDeleting {
		t.Fatalf("GraceExpired: want Deleting, got %q", next)
	}
}

func TestTenantTypedMachine_IsTerminal(t *testing.T) {
	m := NewTenantTypedMachine()
	if !m.IsTerminal(TPhDeleting) {
		t.Error("Deleting should be terminal")
	}
	for _, p := range []TenantPhase{TPhEmpty, TPhProvisioning, TPhActive, TPhSuspended, TPhPendingDeletion} {
		if m.IsTerminal(p) {
			t.Errorf("%q should not be terminal", p)
		}
	}
}

func TestTenantTypedMachine_WireFormat(t *testing.T) {
	pairs := []struct {
		typed   TenantPhase
		untyped State
	}{
		{TPhEmpty, TenantEmpty}, {TPhProvisioning, TenantProvisioning},
		{TPhActive, TenantActive}, {TPhSuspended, TenantSuspended},
		{TPhPendingDeletion, TenantPendingDeletion}, {TPhDeleting, TenantDeleting},
	}
	for _, p := range pairs {
		if string(p.typed) != string(p.untyped) {
			t.Errorf("wire drift: %q != %q", p.typed, p.untyped)
		}
	}
}

// =============================================================================
// Environment
// =============================================================================

func TestEnvironmentTypedMachine_HappyPath(t *testing.T) {
	m := NewEnvironmentTypedMachine()
	next := m.Transition(EPhEmpty, EPEventCreate)
	if next != EPhPending {
		t.Fatalf("Create from Empty: want Pending, got %q", next)
	}
	next = m.Transition(EPhPending, EPEventProvisioned)
	if next != EPhActive {
		t.Fatalf("Provisioned: want Active, got %q", next)
	}
}

func TestEnvironmentTypedMachine_RetryFromFailed(t *testing.T) {
	m := NewEnvironmentTypedMachine()
	// Pending → Failed → Pending (Retry)
	next := m.Transition(EPhPending, EPEventProvisionFailed)
	if next != EPhFailed {
		t.Fatalf("ProvisionFailed: want Failed, got %q", next)
	}
	next = m.Transition(EPhFailed, EPEventRetry)
	if next != EPhPending {
		t.Fatalf("Retry from Failed: want Pending, got %q", next)
	}
}

func TestEnvironmentTypedMachine_WireFormat(t *testing.T) {
	pairs := []struct {
		typed   EnvironmentPhase
		untyped State
	}{
		{EPhEmpty, EnvironmentEmpty}, {EPhPending, EnvironmentPending},
		{EPhActive, EnvironmentActive}, {EPhFailed, EnvironmentFailed},
		{EPhDeleting, EnvironmentDeleting},
	}
	for _, p := range pairs {
		if string(p.typed) != string(p.untyped) {
			t.Errorf("wire drift: %q != %q", p.typed, p.untyped)
		}
	}
}

// =============================================================================
// LoopRuntimeApplication (cyclic — Reachable↔Degraded)
// =============================================================================

func TestApplicationTypedMachine_FullHappyPath(t *testing.T) {
	m := NewApplicationTypedMachine()
	// Empty → Progressing → Serving → Reachable
	steps := []struct {
		from  ApplicationPhase
		event ApplicationPhaseEvent
		want  ApplicationPhase
	}{
		{APhEmpty, APEventCreate, APhProgressing},
		{APhProgressing, APEventEndpointsReady, APhServing},
		{APhServing, APEventRouteOK, APhReachable},
	}
	for _, s := range steps {
		got := m.Transition(s.from, s.event)
		if got != s.want {
			t.Errorf("%q + %q: want %q, got %q", s.from, s.event, s.want, got)
		}
	}
}

func TestApplicationTypedMachine_DegradedCycle(t *testing.T) {
	m := NewApplicationTypedMachine()
	// Reachable → Degraded (RouteFail) → Reachable (RouteOK)
	next := m.Transition(APhReachable, APEventRouteFail)
	if next != APhDegraded {
		t.Fatalf("RouteFail: want Degraded, got %q", next)
	}
	next = m.Transition(APhDegraded, APEventRouteOK)
	if next != APhReachable {
		t.Fatalf("RouteOK from Degraded: want Reachable, got %q", next)
	}
}

func TestApplicationTypedMachine_OrphanedFromEveryPhase(t *testing.T) {
	m := NewApplicationTypedMachine()
	// Orphaned event valid from every non-terminal non-empty state
	phases := []ApplicationPhase{APhProgressing, APhServing, APhReachable, APhDegraded, APhDown}
	for _, p := range phases {
		next := m.Transition(p, APEventOrphaned)
		if next != APhCleaned {
			t.Errorf("Orphaned from %q: want Cleaned, got %q", p, next)
		}
	}
}

func TestApplicationTypedMachine_IsTerminal(t *testing.T) {
	m := NewApplicationTypedMachine()
	if !m.IsTerminal(APhCleaned) {
		t.Error("Cleaned should be terminal")
	}
	for _, p := range []ApplicationPhase{APhEmpty, APhProgressing, APhServing, APhReachable, APhDegraded, APhDown} {
		if m.IsTerminal(p) {
			t.Errorf("%q should NOT be terminal", p)
		}
	}
}

func TestApplicationTypedMachine_WireFormat(t *testing.T) {
	pairs := []struct {
		typed   ApplicationPhase
		untyped State
	}{
		{APhEmpty, ApplicationEmpty}, {APhProgressing, ApplicationProgressing},
		{APhServing, ApplicationServing}, {APhReachable, ApplicationReachable},
		{APhDegraded, ApplicationDegraded}, {APhDown, ApplicationDown},
		{APhCleaned, ApplicationCleaned},
	}
	for _, p := range pairs {
		if string(p.typed) != string(p.untyped) {
			t.Errorf("wire drift: %q != %q", p.typed, p.untyped)
		}
	}
}

// =============================================================================
// Domain (v1 TXT verification + v2 CF Zone)
// =============================================================================

func TestDomainTypedMachine_V1HappyPath(t *testing.T) {
	m := NewDomainTypedMachine()
	// v1: pending_verification → verified → provisioning → active
	steps := []struct {
		from  DomainPhase
		event DomainPhaseEvent
		want  DomainPhase
	}{
		{DPhPendingVerification, DPEventVerify, DPhVerified},
		{DPhVerified, DPEventProvision, DPhProvisioning},
		{DPhProvisioning, DPEventActivate, DPhActive},
	}
	for _, s := range steps {
		got := m.Transition(s.from, s.event)
		if got != s.want {
			t.Errorf("v1 %q + %q: want %q, got %q", s.from, s.event, s.want, got)
		}
	}
}

func TestDomainTypedMachine_V2HappyPath(t *testing.T) {
	m := NewDomainTypedMachine()
	// v2: pending_ns → ns_active → provisioning → active
	steps := []struct {
		from  DomainPhase
		event DomainPhaseEvent
		want  DomainPhase
	}{
		{DPhPendingNS, DPEventNSActivated, DPhNSActive},
		{DPhNSActive, DPEventProvision, DPhProvisioning},
		{DPhProvisioning, DPEventActivate, DPhActive},
	}
	for _, s := range steps {
		got := m.Transition(s.from, s.event)
		if got != s.want {
			t.Errorf("v2 %q + %q: want %q, got %q", s.from, s.event, s.want, got)
		}
	}
}

func TestDomainTypedMachine_DegradedGracePeriod(t *testing.T) {
	m := NewDomainTypedMachine()
	// Active → Degraded (CNAME lost) → Active (recovered) or Failed (lost permanently)
	next := m.Transition(DPhActive, DPEventDegrade)
	if next != DPhDegraded {
		t.Fatalf("Degrade: want Degraded, got %q", next)
	}
	next = m.Transition(DPhDegraded, DPEventCNAMERecovered)
	if next != DPhActive {
		t.Fatalf("CNAMERecovered: want Active, got %q", next)
	}
	// Or eventually Failed (grace expired)
	next = m.Transition(DPhDegraded, DPEventCNAMELost)
	if next != DPhFailed {
		t.Fatalf("CNAMELost: want Failed, got %q", next)
	}
}

func TestDomainTypedMachine_IsTerminal(t *testing.T) {
	m := NewDomainTypedMachine()
	if !m.IsTerminal(DPhRemoving) {
		t.Error("Removing should be terminal")
	}
	if !IsDomainPhaseTerminal(DPhRemoving) {
		t.Error("IsDomainPhaseTerminal(removing) should be true")
	}
	for _, p := range []DomainPhase{
		DPhPendingVerification, DPhVerified, DPhPendingNS, DPhNSActive,
		DPhProvisioning, DPhActive, DPhDegraded, DPhFailed,
	} {
		if m.IsTerminal(p) {
			t.Errorf("%q should not be terminal", p)
		}
	}
}

func TestDomainTypedMachine_WireFormat(t *testing.T) {
	pairs := []struct {
		typed   DomainPhase
		untyped State
	}{
		{DPhPendingVerification, DomainPendingVerification}, {DPhVerified, DomainVerified},
		{DPhPendingNS, DomainPendingNS}, {DPhNSActive, DomainNSActive},
		{DPhProvisioning, DomainProvisioning}, {DPhActive, DomainActive},
		{DPhDegraded, DomainDegraded}, {DPhFailed, DomainFailed},
		{DPhRemoving, DomainRemoving},
	}
	for _, p := range pairs {
		if string(p.typed) != string(p.untyped) {
			t.Errorf("wire drift: %q != %q", p.typed, p.untyped)
		}
	}
}

// =============================================================================
// Cross-machine: every typed wrapper Name() matches its inner Name()
// =============================================================================

func TestPhase3_AllTypedMachines_Name(t *testing.T) {
	cases := []struct {
		name     string
		actual   string
		expected string
	}{
		{"Tenant", NewTenantTypedMachine().Name(), "Tenant"},
		{"Environment", NewEnvironmentTypedMachine().Name(), "Environment"},
		{"Application", NewApplicationTypedMachine().Name(), "LoopRuntimeApplication"},
		{"Domain", NewDomainTypedMachine().Name(), "Domain"},
	}
	for _, c := range cases {
		if c.actual != c.expected {
			t.Errorf("%s.Name() = %q, want %q", c.name, c.actual, c.expected)
		}
	}
}
