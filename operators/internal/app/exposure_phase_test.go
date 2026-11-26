// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package app

import (
	"testing"

	loopzerov1 "github.com/getloopscale/loop-runtime/operators/api/v1"
)

// The ExposedRoute phase is reached only along transitions the ExposedRoute
// state machine declares; the controller supplies observations, never phases.
func TestAdvanceRoutePhase_FollowsTheDeclaredTransitions(t *testing.T) {
	cases := []struct {
		name  string
		phase loopzerov1.ExposedRoutePhase
		obs   routeObservation
		want  loopzerov1.ExposedRoutePhase
	}{
		{"a new route starts Pending", "", routeObservation{}, loopzerov1.ExposedRoutePhasePending},
		{"a route waits in Pending for its certificate", loopzerov1.ExposedRoutePhasePending, routeObservation{}, loopzerov1.ExposedRoutePhasePending},
		{"a Failed route with no certificate retries back to Pending", loopzerov1.ExposedRoutePhaseFailed, routeObservation{}, loopzerov1.ExposedRoutePhasePending},
		{"an issued certificate moves Pending to CertIssued", loopzerov1.ExposedRoutePhasePending, routeObservation{certReady: true}, loopzerov1.ExposedRoutePhaseCertIssued},
		{"a successful probe makes a new route Reachable", "", routeObservation{certReady: true, probeOK: true}, loopzerov1.ExposedRoutePhaseReachable},
		{"a successful probe recovers a Failed route", loopzerov1.ExposedRoutePhaseFailed, routeObservation{certReady: true, probeOK: true}, loopzerov1.ExposedRoutePhaseReachable},
		{"exhausted probes fail a Reachable route", loopzerov1.ExposedRoutePhaseReachable, routeObservation{certReady: true, probesExhausted: true}, loopzerov1.ExposedRoutePhaseFailed},
		{"exhausted probes fail a Pending route after its certificate is issued", loopzerov1.ExposedRoutePhasePending, routeObservation{certReady: true, probesExhausted: true}, loopzerov1.ExposedRoutePhaseFailed},
		{"a single failed probe keeps a Reachable route Reachable", loopzerov1.ExposedRoutePhaseReachable, routeObservation{certReady: true}, loopzerov1.ExposedRoutePhaseReachable},
		{"a Failed route with a failing probe stays Failed", loopzerov1.ExposedRoutePhaseFailed, routeObservation{certReady: true, probesExhausted: true}, loopzerov1.ExposedRoutePhaseFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := advanceRoutePhase(tc.phase, tc.obs)
			if err != nil {
				t.Fatalf("advanceRoutePhase: %v", err)
			}
			if got != tc.want {
				t.Fatalf("phase = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAdvanceRoutePhase_RejectsAPhaseTheMachineDoesNotKnow(t *testing.T) {
	if _, err := advanceRoutePhase("Lost", routeObservation{certReady: true, probeOK: true}); err == nil {
		t.Fatal("an undeclared phase must be an error, not force-set to Reachable")
	}
}
