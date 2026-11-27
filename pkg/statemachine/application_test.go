// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

import "testing"

func TestApplicationMachine_HappyPath(t *testing.T) {
	m := NewApplicationMachine()

	state := m.Transition(ApplicationEmpty, ApplicationCreate)
	assertState(t, state, ApplicationProgressing)

	state = m.Transition(state, ApplicationEndpointsReady)
	assertState(t, state, ApplicationServing)

	state = m.Transition(state, ApplicationRouteOK)
	assertState(t, state, ApplicationReachable)
}

func TestApplicationMachine_ReachableFlappingThroughDegraded(t *testing.T) {
	m := NewApplicationMachine()
	state := m.Transition(ApplicationReachable, ApplicationRouteFail)
	assertState(t, state, ApplicationDegraded)

	state = m.Transition(state, ApplicationRouteOK)
	assertState(t, state, ApplicationReachable)
}

func TestApplicationMachine_EndpointsLostFromAllOnlineStates(t *testing.T) {
	m := NewApplicationMachine()
	for _, from := range []State{ApplicationProgressing, ApplicationServing, ApplicationReachable, ApplicationDegraded} {
		state := m.Transition(from, ApplicationEndpointsLost)
		assertState(t, state, ApplicationDown)
	}
}

func TestApplicationMachine_DownRecovery(t *testing.T) {
	m := NewApplicationMachine()
	state := m.Transition(ApplicationDown, ApplicationEndpointsReady)
	assertState(t, state, ApplicationProgressing)
}

func TestApplicationMachine_UpdateRollsBackToProgressing(t *testing.T) {
	m := NewApplicationMachine()
	for _, from := range []State{ApplicationServing, ApplicationReachable, ApplicationDegraded, ApplicationDown} {
		state := m.Transition(from, ApplicationUpdate)
		assertState(t, state, ApplicationProgressing)
	}
}

func TestApplicationMachine_InvalidTransitionPanics(t *testing.T) {
	m := NewApplicationMachine()
	assertPanics(t, func() {
		// Empty cannot go directly to Reachable.
		m.Transition(ApplicationEmpty, ApplicationRouteOK)
	}, "Empty -> Reachable should panic")
}

func TestApplicationMachine_OnlyCleanedIsTerminal(t *testing.T) {
	m := NewApplicationMachine()
	for _, s := range []State{
		ApplicationProgressing, ApplicationServing, ApplicationReachable,
		ApplicationDegraded, ApplicationDown,
	} {
		if m.IsTerminal(s) {
			t.Errorf("state %q should not be terminal — Application cycles until reaped", s)
		}
	}
	if !m.IsTerminal(ApplicationCleaned) {
		t.Errorf("ApplicationCleaned must be terminal — reaper-driven garbage collection state")
	}
}

// fix-all-platform-issues-plan T2.2 / Compliance review fix 2026-05-03 —
// Cleaned state machine integration test. Catches drift if someone
// removes the Orphaned event or one of the 5 from-state transitions.

func TestApplicationMachine_OrphanedFromAllNonTerminalStates(t *testing.T) {
	m := NewApplicationMachine()
	for _, from := range []State{
		ApplicationProgressing, ApplicationServing, ApplicationReachable,
		ApplicationDegraded, ApplicationDown,
	} {
		state := m.Transition(from, ApplicationOrphaned)
		assertState(t, state, ApplicationCleaned)
	}
}

func TestApplicationMachine_CleanedIsTerminal_NoEventsLeave(t *testing.T) {
	m := NewApplicationMachine()
	// Once Cleaned, NO event should transition out — the reaper finalizes
	// the CR and the controller stops reconciling. Any attempted transition
	// from Cleaned must panic.
	for _, ev := range []Event{
		ApplicationCreate, ApplicationEndpointsReady, ApplicationEndpointsLost,
		ApplicationRouteOK, ApplicationRouteFail, ApplicationUpdate, ApplicationOrphaned,
	} {
		evCopy := ev
		assertPanics(t, func() {
			m.Transition(ApplicationCleaned, evCopy)
		}, "Cleaned should not accept any event (got %q)", evCopy)
	}
}

func TestApplicationMachine_OrphanedNotAllowedFromEmpty(t *testing.T) {
	// Reaper normalizes Empty→Progressing before calling Orphaned (per
	// application_reaper.go MarkAsCleaned), but the state machine itself
	// must REJECT Empty→Orphaned to make the contract explicit.
	m := NewApplicationMachine()
	assertPanics(t, func() {
		m.Transition(ApplicationEmpty, ApplicationOrphaned)
	}, "Empty -> Cleaned via Orphaned should panic — reaper normalizes first")
}
