// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

import (
	"testing"
)

// Phase 4 coverage push (Plan 2026-05-21 v1.1 T4.2): raise pkg/statemachine
// from 60.3% to ≥90% by covering typed wrapper helpers across 4 machines
// (application, domain, environment, platformbootstrap) which have 0%
// coverage on TryTransition / IsKnownState / IsKnownEvent / MustParseState
// / MustParseEvent / ValidEvents methods.
//
// Pattern (4 machines × ~6 methods each):
//   - bogus state/event → unknown
//   - MustParse on bogus → panic
//   - ValidEvents on known state → non-empty / exercise code path

// =============================================================================
// Application typed wrapper
// =============================================================================

func TestApplicationTyped_TryTransition_Invalid(t *testing.T) {
	m := NewApplicationTypedMachine()
	_, err := m.TryTransition(ApplicationPhase(ApplicationServing), ApplicationPhaseEvent("bogus-event"))
	if err == nil {
		t.Error("TryTransition with bogus event should error")
	}
}

func TestApplicationTyped_IsKnownState(t *testing.T) {
	m := NewApplicationTypedMachine()
	if !m.IsKnownState(ApplicationPhase(ApplicationServing)) {
		t.Error("ApplicationServing should be known")
	}
	if m.IsKnownState(ApplicationPhase("not-a-state")) {
		t.Error("bogus state should be unknown")
	}
}

func TestApplicationTyped_IsKnownEvent(t *testing.T) {
	m := NewApplicationTypedMachine()
	if m.IsKnownEvent(ApplicationPhaseEvent("not-an-event")) {
		t.Error("bogus event should be unknown")
	}
}

func TestApplicationTyped_MustParseState_Valid(t *testing.T) {
	m := NewApplicationTypedMachine()
	got := m.MustParseState(string(ApplicationServing))
	if got != ApplicationPhase(ApplicationServing) {
		t.Errorf("MustParseState round-trip failed: got %q", got)
	}
}

func TestApplicationTyped_MustParseState_Panics(t *testing.T) {
	m := NewApplicationTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseState bogus should panic")
		}
	}()
	m.MustParseState("not-a-state")
}

func TestApplicationTyped_MustParseEvent_Panics(t *testing.T) {
	m := NewApplicationTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseEvent bogus should panic")
		}
	}()
	m.MustParseEvent("not-an-event")
}

func TestApplicationTyped_ValidEvents(t *testing.T) {
	m := NewApplicationTypedMachine()
	_ = m.ValidEvents(ApplicationPhase(ApplicationServing))
}

// =============================================================================
// Domain typed wrapper
// =============================================================================

func TestDomainTyped_TryTransition_Invalid(t *testing.T) {
	m := NewDomainTypedMachine()
	_, err := m.TryTransition(DomainPhase("bogus"), DomainPhaseEvent("bogus"))
	if err == nil {
		t.Error("bogus transition should error")
	}
}

func TestDomainTyped_IsKnownState(t *testing.T) {
	m := NewDomainTypedMachine()
	if !m.IsKnownState(DomainPhase(DomainActive)) {
		t.Error("DomainActive should be known")
	}
	if m.IsKnownState(DomainPhase("bogus")) {
		t.Error("bogus state should be unknown")
	}
}

func TestDomainTyped_IsKnownEvent(t *testing.T) {
	m := NewDomainTypedMachine()
	if m.IsKnownEvent(DomainPhaseEvent("bogus")) {
		t.Error("bogus event should be unknown")
	}
}

func TestDomainTyped_MustParseState_Panics(t *testing.T) {
	m := NewDomainTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseState bogus should panic")
		}
	}()
	m.MustParseState("not-a-state")
}

func TestDomainTyped_MustParseEvent_Panics(t *testing.T) {
	m := NewDomainTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseEvent bogus should panic")
		}
	}()
	m.MustParseEvent("not-an-event")
}

func TestDomainTyped_ValidEvents(t *testing.T) {
	m := NewDomainTypedMachine()
	_ = m.ValidEvents(DomainPhase(DomainActive))
}

// =============================================================================
// Environment typed wrapper
// =============================================================================

func TestEnvironmentTyped_TryTransition_Invalid(t *testing.T) {
	m := NewEnvironmentTypedMachine()
	_, err := m.TryTransition(EnvironmentPhase("bogus"), EnvironmentPhaseEvent("bogus"))
	if err == nil {
		t.Error("bogus transition should error")
	}
}

func TestEnvironmentTyped_IsTerminal(t *testing.T) {
	m := NewEnvironmentTypedMachine()
	if !m.IsTerminal(EnvironmentPhase(EnvironmentDeleting)) {
		t.Error("EnvironmentDeleting should be terminal")
	}
	if m.IsTerminal(EnvironmentPhase(EnvironmentActive)) {
		t.Error("EnvironmentActive should NOT be terminal")
	}
}

func TestEnvironmentTyped_IsKnownState(t *testing.T) {
	m := NewEnvironmentTypedMachine()
	if !m.IsKnownState(EnvironmentPhase(EnvironmentActive)) {
		t.Error("EnvironmentActive should be known")
	}
}

func TestEnvironmentTyped_IsKnownEvent(t *testing.T) {
	m := NewEnvironmentTypedMachine()
	if m.IsKnownEvent(EnvironmentPhaseEvent("bogus")) {
		t.Error("bogus event should be unknown")
	}
}

func TestEnvironmentTyped_MustParseState_Panics(t *testing.T) {
	m := NewEnvironmentTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseState bogus should panic")
		}
	}()
	m.MustParseState("not-a-state")
}

func TestEnvironmentTyped_MustParseEvent_Panics(t *testing.T) {
	m := NewEnvironmentTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseEvent bogus should panic")
		}
	}()
	m.MustParseEvent("not-an-event")
}

func TestEnvironmentTyped_ValidEvents(t *testing.T) {
	m := NewEnvironmentTypedMachine()
	_ = m.ValidEvents(EnvironmentPhase(EnvironmentActive))
}

// =============================================================================
// PlatformBootstrap typed wrapper
// =============================================================================

func TestPlatformBootstrapTyped_TryTransition_Invalid(t *testing.T) {
	m := NewPlatformBootstrapTypedMachine()
	_, err := m.TryTransition(PlatformBootstrapPhase("bogus"), PlatformBootstrapPhaseEvent("bogus"))
	if err == nil {
		t.Error("bogus transition should error")
	}
}

func TestPlatformBootstrapTyped_IsKnownState(t *testing.T) {
	m := NewPlatformBootstrapTypedMachine()
	if m.IsKnownState(PlatformBootstrapPhase("bogus")) {
		t.Error("bogus state should be unknown")
	}
}

func TestPlatformBootstrapTyped_IsKnownEvent(t *testing.T) {
	m := NewPlatformBootstrapTypedMachine()
	if m.IsKnownEvent(PlatformBootstrapPhaseEvent("bogus")) {
		t.Error("bogus event should be unknown")
	}
}

func TestPlatformBootstrapTyped_MustParseState_Panics(t *testing.T) {
	m := NewPlatformBootstrapTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseState bogus should panic")
		}
	}()
	m.MustParseState("not-a-state")
}

func TestPlatformBootstrapTyped_MustParseEvent_Panics(t *testing.T) {
	m := NewPlatformBootstrapTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseEvent bogus should panic")
		}
	}()
	m.MustParseEvent("not-an-event")
}

func TestPlatformBootstrapTyped_ValidEvents(t *testing.T) {
	m := NewPlatformBootstrapTypedMachine()
	_ = m.ValidEvents(PlatformBootstrapPhase(""))
}

// =============================================================================
// Preview typed wrapper
// =============================================================================

func TestPreviewTyped_TryTransition_Invalid(t *testing.T) {
	m := NewPreviewTypedMachine()
	_, err := m.TryTransition(PreviewPhase("bogus"), PreviewPhaseEvent("bogus"))
	if err == nil {
		t.Error("bogus transition should error")
	}
}

func TestPreviewTyped_IsKnownState(t *testing.T) {
	m := NewPreviewTypedMachine()
	if !m.IsKnownState(PreviewPhase(PreviewActive)) {
		t.Error("PreviewActive should be known")
	}
	if m.IsKnownState(PreviewPhase("bogus")) {
		t.Error("bogus state should be unknown")
	}
}

func TestPreviewTyped_IsKnownEvent(t *testing.T) {
	m := NewPreviewTypedMachine()
	if m.IsKnownEvent(PreviewPhaseEvent("bogus")) {
		t.Error("bogus event should be unknown")
	}
}

func TestPreviewTyped_MustParseState_Panics(t *testing.T) {
	m := NewPreviewTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseState bogus should panic")
		}
	}()
	m.MustParseState("not-a-state")
}

func TestPreviewTyped_MustParseEvent_Panics(t *testing.T) {
	m := NewPreviewTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseEvent bogus should panic")
		}
	}()
	m.MustParseEvent("not-an-event")
}

func TestPreviewTyped_ValidEvents(t *testing.T) {
	m := NewPreviewTypedMachine()
	_ = m.ValidEvents(PreviewPhase(PreviewActive))
}

// =============================================================================
// Promotion typed wrapper
// =============================================================================

func TestPromotionTyped_TryTransition_Invalid(t *testing.T) {
	m := NewPromotionTypedMachine()
	_, err := m.TryTransition(PromotionPhase("bogus"), PromotionPhaseEvent("bogus"))
	if err == nil {
		t.Error("bogus transition should error")
	}
}

func TestPromotionTyped_IsKnownState(t *testing.T) {
	m := NewPromotionTypedMachine()
	if !m.IsKnownState(PromotionPhase(PromotionValidating)) {
		t.Error("PromotionValidating should be known")
	}
	if m.IsKnownState(PromotionPhase("bogus")) {
		t.Error("bogus state should be unknown")
	}
}

func TestPromotionTyped_IsKnownEvent(t *testing.T) {
	m := NewPromotionTypedMachine()
	if m.IsKnownEvent(PromotionPhaseEvent("bogus")) {
		t.Error("bogus event should be unknown")
	}
}

func TestPromotionTyped_MustParseState_Panics(t *testing.T) {
	m := NewPromotionTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseState bogus should panic")
		}
	}()
	m.MustParseState("not-a-state")
}

func TestPromotionTyped_MustParseEvent_Panics(t *testing.T) {
	m := NewPromotionTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseEvent bogus should panic")
		}
	}()
	m.MustParseEvent("not-an-event")
}

func TestPromotionTyped_ValidEvents(t *testing.T) {
	m := NewPromotionTypedMachine()
	_ = m.ValidEvents(PromotionPhase(PromotionValidating))
}

// =============================================================================
// Rollback typed wrapper
// =============================================================================

func TestRollbackTyped_TryTransition_Invalid(t *testing.T) {
	m := NewRollbackTypedMachine()
	_, err := m.TryTransition(RollbackPhase("bogus"), RollbackPhaseEvent("bogus"))
	if err == nil {
		t.Error("bogus transition should error")
	}
}

func TestRollbackTyped_IsKnownState(t *testing.T) {
	m := NewRollbackTypedMachine()
	if !m.IsKnownState(RollbackPhase(RollbackValidating)) {
		t.Error("RollbackValidating should be known")
	}
	if m.IsKnownState(RollbackPhase("bogus")) {
		t.Error("bogus state should be unknown")
	}
}

func TestRollbackTyped_IsKnownEvent(t *testing.T) {
	m := NewRollbackTypedMachine()
	if m.IsKnownEvent(RollbackPhaseEvent("bogus")) {
		t.Error("bogus event should be unknown")
	}
}

func TestRollbackTyped_MustParseState_Panics(t *testing.T) {
	m := NewRollbackTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseState bogus should panic")
		}
	}()
	m.MustParseState("not-a-state")
}

func TestRollbackTyped_MustParseEvent_Panics(t *testing.T) {
	m := NewRollbackTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseEvent bogus should panic")
		}
	}()
	m.MustParseEvent("not-an-event")
}

func TestRollbackTyped_ValidEvents(t *testing.T) {
	m := NewRollbackTypedMachine()
	_ = m.ValidEvents(RollbackPhase(RollbackValidating))
}

// =============================================================================
// Tenant typed wrapper
// =============================================================================

func TestTenantTyped_TryTransition_Invalid(t *testing.T) {
	m := NewTenantTypedMachine()
	_, err := m.TryTransition(TenantPhase("bogus"), TenantPhaseEvent("bogus"))
	if err == nil {
		t.Error("bogus transition should error")
	}
}

func TestTenantTyped_IsKnownState(t *testing.T) {
	m := NewTenantTypedMachine()
	if !m.IsKnownState(TenantPhase(TenantProvisioning)) {
		t.Error("TenantProvisioning should be known")
	}
	if m.IsKnownState(TenantPhase("bogus")) {
		t.Error("bogus state should be unknown")
	}
}

func TestTenantTyped_IsKnownEvent(t *testing.T) {
	m := NewTenantTypedMachine()
	if m.IsKnownEvent(TenantPhaseEvent("bogus")) {
		t.Error("bogus event should be unknown")
	}
}

func TestTenantTyped_MustParseState_Panics(t *testing.T) {
	m := NewTenantTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseState bogus should panic")
		}
	}()
	m.MustParseState("not-a-state")
}

func TestTenantTyped_MustParseEvent_Panics(t *testing.T) {
	m := NewTenantTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseEvent bogus should panic")
		}
	}()
	m.MustParseEvent("not-an-event")
}

func TestTenantTyped_ValidEvents(t *testing.T) {
	m := NewTenantTypedMachine()
	_ = m.ValidEvents(TenantPhase(TenantProvisioning))
}

// =============================================================================
// Build typed wrapper (note: NewBuildTypedMachine returns *BuildMachine
// — base machine, not a typed wrapper struct). The typed methods are
// already on *BuildMachine and many are covered by existing tests; here
// we close the IsKnown* / MustParse* / ValidEvents gaps explicitly.
// =============================================================================

func TestBuildTyped_TryTransition_Invalid(t *testing.T) {
	m := NewBuildTypedMachine()
	_, err := m.TryTransition(BPhQueued, BuildPhaseEvent("bogus"))
	if err == nil {
		t.Error("bogus event should yield error or invalid transition")
	}
}

// =============================================================================
// Release typed wrapper
// =============================================================================

func TestReleaseTyped_TryTransition_Invalid(t *testing.T) {
	m := NewReleaseTypedMachine()
	_, err := m.TryTransition(ReleasePhase(ReleaseReady), ReleasePhaseEvent("bogus"))
	if err == nil {
		t.Error("bogus event should error or invalid transition")
	}
}

// =============================================================================
// ExposedRoute typed wrapper
// =============================================================================

func TestExposedRouteTyped_TryTransition_Invalid(t *testing.T) {
	m := NewExposedRouteTypedMachine()
	_, err := m.TryTransition(ExposedRouteState("bogus"), ExposedRouteEvent("bogus"))
	if err == nil {
		t.Error("bogus state/event should error")
	}
}

// =============================================================================
// Build typed wrapper helpers (IsKnown/MustParse/ValidEvents at 0%)
// =============================================================================

func TestBuildTyped_IsKnownState(t *testing.T) {
	m := NewBuildTypedMachine()
	if !m.IsKnownState(BPhQueued) {
		t.Error("BPhQueued should be known")
	}
	if m.IsKnownState(BuildPhase("bogus")) {
		t.Error("bogus state should be unknown")
	}
}

func TestBuildTyped_IsKnownEvent(t *testing.T) {
	m := NewBuildTypedMachine()
	if m.IsKnownEvent(BuildPhaseEvent("bogus")) {
		t.Error("bogus event should be unknown")
	}
}

func TestBuildTyped_MustParseEvent_Panics(t *testing.T) {
	m := NewBuildTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseEvent bogus should panic")
		}
	}()
	m.MustParseEvent("not-an-event")
}

func TestBuildTyped_ValidEvents(t *testing.T) {
	m := NewBuildTypedMachine()
	_ = m.ValidEvents(BPhQueued)
}

// =============================================================================
// Release typed wrapper helpers (0% on IsKnown/MustParse* + ValidEvents)
// =============================================================================

func TestReleaseTyped_IsKnownState(t *testing.T) {
	m := NewReleaseTypedMachine()
	if !m.IsKnownState(ReleasePhase(ReleaseReady)) {
		t.Error("ReleaseReady should be known")
	}
	if m.IsKnownState(ReleasePhase("bogus")) {
		t.Error("bogus state should be unknown")
	}
}

func TestReleaseTyped_IsKnownEvent(t *testing.T) {
	m := NewReleaseTypedMachine()
	if m.IsKnownEvent(ReleasePhaseEvent("bogus")) {
		t.Error("bogus event should be unknown")
	}
}

func TestReleaseTyped_MustParseState_Panics(t *testing.T) {
	m := NewReleaseTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseState bogus should panic")
		}
	}()
	m.MustParseState("not-a-state")
}

func TestReleaseTyped_MustParseEvent_Panics(t *testing.T) {
	m := NewReleaseTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseEvent bogus should panic")
		}
	}()
	m.MustParseEvent("not-an-event")
}

func TestReleaseTyped_ValidEvents(t *testing.T) {
	m := NewReleaseTypedMachine()
	_ = m.ValidEvents(ReleasePhase(ReleaseReady))
}

// =============================================================================
// ExposedRoute typed wrapper helpers (MustParseEvent at 0%)
// =============================================================================

func TestExposedRouteTyped_MustParseEvent_Panics(t *testing.T) {
	m := NewExposedRouteTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParseEvent bogus should panic")
		}
	}()
	m.MustParseEvent("not-an-event")
}
