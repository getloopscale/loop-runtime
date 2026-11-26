// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

import (
	"testing"
)

// Phase 1 POC tests for ADR `2026-05-15-T6-1-typed-state-machines.md`.
// Validates that the typed wrapper exposes the same lifecycle as the
// untyped Machine while blocking cross-machine type confusion at
// compile time.

func TestExposedRouteTypedMachine_HappyPath(t *testing.T) {
	m := NewExposedRouteTypedMachine()

	// Empty → Pending
	next := m.Transition(XRStateEmpty, XREventCreate)
	if next != XRStatePending {
		t.Fatalf("Create from Empty: want Pending, got %q", next)
	}

	// Pending → CertIssued
	next = m.Transition(XRStatePending, XREventCertReady)
	if next != XRStateCertIssued {
		t.Fatalf("CertReady from Pending: want CertIssued, got %q", next)
	}

	// CertIssued → Reachable
	next = m.Transition(XRStateCertIssued, XREventProbeOK)
	if next != XRStateReachable {
		t.Fatalf("ProbeOK from CertIssued: want Reachable, got %q", next)
	}

	// Reachable → Failed (probe drift)
	next = m.Transition(XRStateReachable, XREventProbeFail)
	if next != XRStateFailed {
		t.Fatalf("ProbeFail from Reachable: want Failed, got %q", next)
	}

	// Failed → Pending (retry)
	next = m.Transition(XRStateFailed, XREventRetry)
	if next != XRStatePending {
		t.Fatalf("Retry from Failed: want Pending, got %q", next)
	}
}

func TestExposedRouteTypedMachine_TryTransition_Invalid(t *testing.T) {
	m := NewExposedRouteTypedMachine()

	// CertReady is not valid from Empty.
	_, err := m.TryTransition(XRStateEmpty, XREventCertReady)
	if err == nil {
		t.Fatal("TryTransition Empty+CertReady: expected error, got nil")
	}
}

func TestExposedRouteTypedMachine_IsTerminal_NeverTrue(t *testing.T) {
	m := NewExposedRouteTypedMachine()
	for _, s := range []ExposedRouteState{
		XRStateEmpty, XRStatePending, XRStateCertIssued,
		XRStateReachable, XRStateFailed,
	} {
		if m.IsTerminal(s) {
			t.Errorf("ExposedRoute has no terminal states by design — got terminal for %q", s)
		}
	}
}

func TestExposedRouteTypedMachine_Name(t *testing.T) {
	m := NewExposedRouteTypedMachine()
	if m.Name() != "ExposedRoute" {
		t.Errorf("Name() = %q, want %q", m.Name(), "ExposedRoute")
	}
}

func TestExposedRouteTypedMachine_IsKnownState(t *testing.T) {
	m := NewExposedRouteTypedMachine()
	for _, s := range []ExposedRouteState{
		XRStateEmpty, XRStatePending, XRStateCertIssued,
		XRStateReachable, XRStateFailed,
	} {
		if !m.IsKnownState(s) {
			t.Errorf("IsKnownState(%q) = false, want true", s)
		}
	}
	if m.IsKnownState(ExposedRouteState("Bogus")) {
		t.Error("IsKnownState(Bogus) = true, want false")
	}
}

func TestExposedRouteTypedMachine_IsKnownEvent(t *testing.T) {
	m := NewExposedRouteTypedMachine()
	for _, e := range []ExposedRouteEvent{
		XREventCreate, XREventCertReady, XREventProbeOK,
		XREventProbeFail, XREventRetry,
	} {
		if !m.IsKnownEvent(e) {
			t.Errorf("IsKnownEvent(%q) = false, want true", e)
		}
	}
	if m.IsKnownEvent(ExposedRouteEvent("Detonate")) {
		t.Error("IsKnownEvent(Detonate) = true, want false")
	}
}

func TestExposedRouteTypedMachine_MustParseState_Valid(t *testing.T) {
	m := NewExposedRouteTypedMachine()
	got := m.MustParseState("Pending")
	if got != XRStatePending {
		t.Errorf("MustParseState(Pending) = %q, want %q", got, XRStatePending)
	}
}

func TestExposedRouteTypedMachine_MustParseState_PanicsOnUnknown(t *testing.T) {
	m := NewExposedRouteTypedMachine()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("MustParseState(Bogus): expected panic, got none")
		}
	}()
	_ = m.MustParseState("Bogus")
}

func TestExposedRouteTypedMachine_ValidEvents(t *testing.T) {
	m := NewExposedRouteTypedMachine()
	events := m.ValidEvents(XRStatePending)
	if len(events) != 2 {
		t.Fatalf("ValidEvents(Pending) = %d events, want 2", len(events))
	}
	// Order is map-iteration dependent; collect to set.
	seen := map[ExposedRouteEvent]bool{}
	for _, e := range events {
		seen[e] = true
	}
	if !seen[XREventCertReady] || !seen[XREventProbeFail] {
		t.Errorf("ValidEvents(Pending) missing expected events; got %v", events)
	}
}

func TestExposedRouteTypedMachine_WireFormatCompatibility(t *testing.T) {
	// Sanity: the typed wire format (string values) MUST match the
	// untyped constants byte-for-byte, so DB rows / CR status fields
	// persisted under the untyped path round-trip through the typed
	// path without translation. This is a critical contract for the
	// coexistence phase before all 11 machines are migrated.
	pairs := []struct {
		typed   ExposedRouteState
		untyped State
	}{
		{XRStateEmpty, ExposedRouteEmpty},
		{XRStatePending, ExposedRoutePending},
		{XRStateCertIssued, ExposedRouteCertIssued},
		{XRStateReachable, ExposedRouteReachable},
		{XRStateFailed, ExposedRouteFailed},
	}
	for _, p := range pairs {
		if string(p.typed) != string(p.untyped) {
			t.Errorf("wire format drift: typed %q != untyped %q", p.typed, p.untyped)
		}
	}

	eventPairs := []struct {
		typed   ExposedRouteEvent
		untyped Event
	}{
		{XREventCreate, ExposedRouteCreate},
		{XREventCertReady, ExposedRouteCertReady},
		{XREventProbeOK, ExposedRouteProbeOK},
		{XREventProbeFail, ExposedRouteProbeFail},
		{XREventRetry, ExposedRouteRetry},
	}
	for _, p := range eventPairs {
		if string(p.typed) != string(p.untyped) {
			t.Errorf("event wire format drift: typed %q != untyped %q", p.typed, p.untyped)
		}
	}
}
