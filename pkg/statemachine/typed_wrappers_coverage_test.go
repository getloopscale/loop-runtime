// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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
