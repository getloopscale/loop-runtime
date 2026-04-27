// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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

func TestApplicationMachine_NoTerminalAllStatesProgress(t *testing.T) {
	m := NewApplicationMachine()
	for _, s := range []State{
		ApplicationProgressing, ApplicationServing, ApplicationReachable,
		ApplicationDegraded, ApplicationDown,
	} {
		if m.IsTerminal(s) {
			t.Errorf("state %q should not be terminal — Application cycles until deletion", s)
		}
	}
}
