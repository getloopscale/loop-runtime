// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package statemachine

import "testing"

func TestExposedRouteMachine_HappyPath(t *testing.T) {
	m := NewExposedRouteMachine()

	state := m.Transition(ExposedRouteEmpty, ExposedRouteCreate)
	assertState(t, state, ExposedRoutePending)

	state = m.Transition(state, ExposedRouteCertReady)
	assertState(t, state, ExposedRouteCertIssued)

	state = m.Transition(state, ExposedRouteProbeOK)
	assertState(t, state, ExposedRouteReachable)
}

func TestExposedRouteMachine_PendingProbeFail(t *testing.T) {
	m := NewExposedRouteMachine()
	state := m.Transition(ExposedRoutePending, ExposedRouteProbeFail)
	assertState(t, state, ExposedRouteFailed)
}

func TestExposedRouteMachine_FailedRetry(t *testing.T) {
	m := NewExposedRouteMachine()
	state := m.Transition(ExposedRouteFailed, ExposedRouteRetry)
	assertState(t, state, ExposedRoutePending)
}

func TestExposedRouteMachine_ReachableFlappingToFailed(t *testing.T) {
	m := NewExposedRouteMachine()
	state := m.Transition(ExposedRouteReachable, ExposedRouteProbeFail)
	assertState(t, state, ExposedRouteFailed)
}

func TestExposedRouteMachine_InvalidTransitionPanics(t *testing.T) {
	m := NewExposedRouteMachine()
	assertPanics(t, func() {
		// Empty cannot go directly to Reachable.
		m.Transition(ExposedRouteEmpty, ExposedRouteProbeOK)
	}, "Empty -> Reachable should panic")
}

func TestExposedRouteMachine_NoTerminalAllStatesProgress(t *testing.T) {
	m := NewExposedRouteMachine()
	for _, s := range []State{ExposedRoutePending, ExposedRouteCertIssued, ExposedRouteReachable, ExposedRouteFailed} {
		if m.IsTerminal(s) {
			t.Errorf("state %q should not be terminal — ExposedRoute is GC'd via ownerRef", s)
		}
	}
}
