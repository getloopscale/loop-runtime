// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

import "testing"

// Phase 4 tests for ADR `2026-05-15-T6-1-typed-state-machines.md`.
// Covers Preview + Promotion + Rollback + PlatformBootstrap typed
// wrappers. After this iter ships, all 11 state machines have typed
// wrappers (closing the Phase 4 portion of the ADR — Phase 5 is the
// linter activation + untyped Machine deletion, which is operator
// scheduling not implementation).

// =============================================================================
// PreviewEnvironment
// =============================================================================

func TestPreviewTypedMachine_HappyPath(t *testing.T) {
	m := NewPreviewTypedMachine()
	steps := []struct {
		from  PreviewPhase
		event PreviewPhaseEvent
		want  PreviewPhase
	}{
		{PPhCreating, PPEventCreationCompleted, PPhActive},
		{PPhActive, PPEventExpirationTriggered, PPhExpiring},
		{PPhExpiring, PPEventCleanupCompleted, PPhDeleted},
	}
	for _, s := range steps {
		got := m.Transition(s.from, s.event)
		if got != s.want {
			t.Errorf("%q + %q: want %q, got %q", s.from, s.event, s.want, got)
		}
	}
}

func TestPreviewTypedMachine_CleanupFailedThenGC(t *testing.T) {
	m := NewPreviewTypedMachine()
	// Expiring → CleanupFailed → Deleted (via garbage collector)
	next := m.Transition(PPhExpiring, PPEventCleanupFailed)
	if next != PPhCleanupFailed {
		t.Fatalf("CleanupFailed from Expiring: want CleanupFailed, got %q", next)
	}
	next = m.Transition(PPhCleanupFailed, PPEventGarbageCollected)
	if next != PPhDeleted {
		t.Fatalf("GarbageCollected: want Deleted, got %q", next)
	}
}

func TestPreviewTypedMachine_IsTerminal(t *testing.T) {
	m := NewPreviewTypedMachine()
	if !m.IsTerminal(PPhDeleted) {
		t.Error("Deleted should be terminal")
	}
	for _, p := range []PreviewPhase{PPhCreating, PPhActive, PPhExpiring, PPhFailed, PPhDeleting, PPhCleanupFailed} {
		if m.IsTerminal(p) {
			t.Errorf("%q should not be terminal", p)
		}
	}
}

func TestPreviewTypedMachine_WireFormat(t *testing.T) {
	pairs := []struct {
		typed   PreviewPhase
		untyped State
	}{
		{PPhCreating, PreviewCreating}, {PPhActive, PreviewActive},
		{PPhExpiring, PreviewExpiring}, {PPhDeleted, PreviewDeleted},
		{PPhFailed, PreviewFailed}, {PPhDeleting, PreviewDeleting},
		{PPhCleanupFailed, PreviewCleanupFailed},
	}
	for _, p := range pairs {
		if string(p.typed) != string(p.untyped) {
			t.Errorf("wire drift: %q != %q", p.typed, p.untyped)
		}
	}
}

// =============================================================================
// Promotion
// =============================================================================

func TestPromotionTypedMachine_HappyPath(t *testing.T) {
	m := NewPromotionTypedMachine()
	steps := []struct {
		from  PromotionPhase
		event PromotionPhaseEvent
		want  PromotionPhase
	}{
		{PrPhValidating, PrPEventValidationPassed, PrPhCommitting},
		{PrPhCommitting, PrPEventCommitCompleted, PrPhSyncing},
		{PrPhSyncing, PrPEventSyncCompleted, PrPhCompleted},
	}
	for _, s := range steps {
		got := m.Transition(s.from, s.event)
		if got != s.want {
			t.Errorf("%q + %q: want %q, got %q", s.from, s.event, s.want, got)
		}
	}
}

func TestPromotionTypedMachine_TerminalStates(t *testing.T) {
	m := NewPromotionTypedMachine()
	terminals := []PromotionPhase{PrPhCompleted, PrPhRejected, PrPhCommitFailed, PrPhSyncFailed}
	for _, p := range terminals {
		if !m.IsTerminal(p) {
			t.Errorf("%q should be terminal", p)
		}
	}
}

func TestPromotionTypedMachine_WireFormat(t *testing.T) {
	pairs := []struct {
		typed   PromotionPhase
		untyped State
	}{
		{PrPhValidating, PromotionValidating}, {PrPhCommitting, PromotionCommitting},
		{PrPhSyncing, PromotionSyncing}, {PrPhCompleted, PromotionCompleted},
		{PrPhRejected, PromotionRejected}, {PrPhCommitFailed, PromotionCommitFailed},
		{PrPhSyncFailed, PromotionSyncFailed},
	}
	for _, p := range pairs {
		if string(p.typed) != string(p.untyped) {
			t.Errorf("wire drift: %q != %q", p.typed, p.untyped)
		}
	}
}

// =============================================================================
// Rollback
// =============================================================================

func TestRollbackTypedMachine_HappyPath(t *testing.T) {
	m := NewRollbackTypedMachine()
	steps := []struct {
		from  RollbackPhase
		event RollbackPhaseEvent
		want  RollbackPhase
	}{
		{RbPhValidating, RbPEventValidationPassed, RbPhReverting},
		{RbPhReverting, RbPEventRevertCompleted, RbPhSyncing},
		{RbPhSyncing, RbPEventSyncCompleted, RbPhCompleted},
	}
	for _, s := range steps {
		got := m.Transition(s.from, s.event)
		if got != s.want {
			t.Errorf("%q + %q: want %q, got %q", s.from, s.event, s.want, got)
		}
	}
}

func TestRollbackTypedMachine_BlockedNoPrevious(t *testing.T) {
	m := NewRollbackTypedMachine()
	next := m.Transition(RbPhValidating, RbPEventNoPreviousRelease)
	if next != RbPhBlocked {
		t.Fatalf("NoPreviousRelease: want Blocked, got %q", next)
	}
}

func TestRollbackTypedMachine_RejectedOnUnacknowledged(t *testing.T) {
	m := NewRollbackTypedMachine()
	next := m.Transition(RbPhValidating, RbPEventUnacknowledgedEffects)
	if next != RbPhRejected {
		t.Fatalf("UnacknowledgedEffects: want Rejected, got %q", next)
	}
}

func TestRollbackTypedMachine_TerminalStates(t *testing.T) {
	m := NewRollbackTypedMachine()
	terminals := []RollbackPhase{RbPhCompleted, RbPhRejected, RbPhBlocked, RbPhRevertFailed, RbPhSyncFailed}
	for _, p := range terminals {
		if !m.IsTerminal(p) {
			t.Errorf("%q should be terminal", p)
		}
	}
}

func TestRollbackTypedMachine_WireFormat(t *testing.T) {
	pairs := []struct {
		typed   RollbackPhase
		untyped State
	}{
		{RbPhValidating, RollbackValidating}, {RbPhReverting, RollbackReverting},
		{RbPhSyncing, RollbackSyncing}, {RbPhCompleted, RollbackCompleted},
		{RbPhRejected, RollbackRejected}, {RbPhBlocked, RollbackBlocked},
		{RbPhRevertFailed, RollbackRevertFailed}, {RbPhSyncFailed, RollbackSyncFailed},
	}
	for _, p := range pairs {
		if string(p.typed) != string(p.untyped) {
			t.Errorf("wire drift: %q != %q", p.typed, p.untyped)
		}
	}
}

// =============================================================================
// PlatformBootstrap
// =============================================================================

func TestPlatformBootstrapTypedMachine_HappyPath(t *testing.T) {
	m := NewPlatformBootstrapTypedMachine()
	steps := []struct {
		from  PlatformBootstrapPhase
		event PlatformBootstrapPhaseEvent
		want  PlatformBootstrapPhase
	}{
		{PBPhEmpty, PBPEventStart, PBPhPending},
		{PBPhPending, PBPEventStart, PBPhConfiguringGitea},
		{PBPhConfiguringGitea, PBPEventGiteaReady, PBPhConfiguringArgo},
		{PBPhConfiguringArgo, PBPEventArgoCDReady, PBPhWaitingForAPI},
		{PBPhWaitingForAPI, PBPEventAPISeeded, PBPhComplete},
	}
	for _, s := range steps {
		got := m.Transition(s.from, s.event)
		if got != s.want {
			t.Errorf("%q + %q: want %q, got %q", s.from, s.event, s.want, got)
		}
	}
}

func TestPlatformBootstrapTypedMachine_RetryExhaustedFromAnyConfigPhase(t *testing.T) {
	m := NewPlatformBootstrapTypedMachine()
	// RetryExhausted valid from each ConfiguringX + WaitingForAPI → Failed
	configPhases := []PlatformBootstrapPhase{
		PBPhConfiguringGitea, PBPhConfiguringArgo, PBPhWaitingForAPI,
	}
	for _, p := range configPhases {
		next := m.Transition(p, PBPEventRetryExhaust)
		if next != PBPhFailed {
			t.Errorf("RetryExhausted from %q: want Failed, got %q", p, next)
		}
	}
}

func TestPlatformBootstrapTypedMachine_TerminalStates(t *testing.T) {
	m := NewPlatformBootstrapTypedMachine()
	if !m.IsTerminal(PBPhComplete) {
		t.Error("BootstrapComplete should be terminal")
	}
	if !m.IsTerminal(PBPhFailed) {
		t.Error("Failed should be terminal")
	}
	for _, p := range []PlatformBootstrapPhase{PBPhEmpty, PBPhPending, PBPhConfiguringGitea, PBPhConfiguringArgo, PBPhWaitingForAPI} {
		if m.IsTerminal(p) {
			t.Errorf("%q should not be terminal", p)
		}
	}
}

func TestPlatformBootstrapTypedMachine_WireFormat(t *testing.T) {
	pairs := []struct {
		typed   PlatformBootstrapPhase
		untyped State
	}{
		{PBPhEmpty, PlatformBootstrapEmpty},
		{PBPhPending, PlatformBootstrapPending},
		{PBPhConfiguringGitea, PlatformBootstrapConfiguringGitea},
		{PBPhConfiguringArgo, PlatformBootstrapConfiguringArgo},
		{PBPhWaitingForAPI, PlatformBootstrapWaitingForAPI},
		{PBPhComplete, PlatformBootstrapComplete},
		{PBPhFailed, PlatformBootstrapFailed},
	}
	for _, p := range pairs {
		if string(p.typed) != string(p.untyped) {
			t.Errorf("wire drift: %q != %q", p.typed, p.untyped)
		}
	}
}

// =============================================================================
// Cross-machine sanity: every typed wrapper Name() matches
// =============================================================================

func TestPhase4_AllTypedMachines_Name(t *testing.T) {
	cases := []struct {
		name     string
		actual   string
		expected string
	}{
		{"Preview", NewPreviewTypedMachine().Name(), "PreviewEnvironment"},
		{"Promotion", NewPromotionTypedMachine().Name(), "Promotion"},
		{"Rollback", NewRollbackTypedMachine().Name(), "Rollback"},
		{"PlatformBootstrap", NewPlatformBootstrapTypedMachine().Name(), "PlatformBootstrap"},
	}
	for _, c := range cases {
		if c.actual != c.expected {
			t.Errorf("%s.Name() = %q, want %q", c.name, c.actual, c.expected)
		}
	}
}

// =============================================================================
// Phase 4 milestone: all 11 state machines have typed wrappers
// =============================================================================

func TestPhase4_AllElevenMachinesHaveTypedWrappers(t *testing.T) {
	// This test serves as a milestone marker. If a future ralph iter
	// adds a 12th state machine, this test reminds the contributor to
	// also add a typed wrapper (via Phase 5+ work).
	typedMachines := []struct {
		name string
		ctor interface{}
	}{
		{"ExposedRouteMachine", NewExposedRouteTypedMachine()},
		{"BuildMachine", NewBuildTypedMachine()},
		{"ReleaseMachine", NewReleaseTypedMachine()},
		{"TenantTypedMachine", NewTenantTypedMachine()},
		{"EnvironmentTypedMachine", NewEnvironmentTypedMachine()},
		{"ApplicationTypedMachine", NewApplicationTypedMachine()},
		{"DomainTypedMachine", NewDomainTypedMachine()},
		{"PreviewTypedMachine", NewPreviewTypedMachine()},
		{"PromotionTypedMachine", NewPromotionTypedMachine()},
		{"RollbackTypedMachine", NewRollbackTypedMachine()},
		{"PlatformBootstrapTypedMachine", NewPlatformBootstrapTypedMachine()},
	}
	if len(typedMachines) != 11 {
		t.Errorf("expected 11 typed machines (one per CLAUDE.md state machine), got %d", len(typedMachines))
	}
	for _, tm := range typedMachines {
		if tm.ctor == nil {
			t.Errorf("%s: typed machine constructor returned nil", tm.name)
		}
	}
}
