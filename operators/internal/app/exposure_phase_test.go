// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package app

import (
	"testing"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
)

// The ExposedRoute phase is reached only along transitions the ExposedRoute
// state machine declares; the controller supplies observations, never phases.
func TestAdvanceRoutePhase_FollowsTheDeclaredTransitions(t *testing.T) {
	cases := []struct {
		name  string
		phase loopruntimev1.ExposedRoutePhase
		obs   routeObservation
		want  loopruntimev1.ExposedRoutePhase
	}{
		{"a new route starts Pending", "", routeObservation{}, loopruntimev1.ExposedRoutePhasePending},
		{"a route waits in Pending for its certificate", loopruntimev1.ExposedRoutePhasePending, routeObservation{}, loopruntimev1.ExposedRoutePhasePending},
		{"a Failed route with no certificate retries back to Pending", loopruntimev1.ExposedRoutePhaseFailed, routeObservation{}, loopruntimev1.ExposedRoutePhasePending},
		{"an issued certificate moves Pending to CertIssued", loopruntimev1.ExposedRoutePhasePending, routeObservation{certReady: true}, loopruntimev1.ExposedRoutePhaseCertIssued},
		{"a successful probe makes a new route Reachable", "", routeObservation{certReady: true, probeOK: true}, loopruntimev1.ExposedRoutePhaseReachable},
		{"a successful probe recovers a Failed route", loopruntimev1.ExposedRoutePhaseFailed, routeObservation{certReady: true, probeOK: true}, loopruntimev1.ExposedRoutePhaseReachable},
		{"exhausted probes fail a Reachable route", loopruntimev1.ExposedRoutePhaseReachable, routeObservation{certReady: true, probesExhausted: true}, loopruntimev1.ExposedRoutePhaseFailed},
		{"exhausted probes fail a Pending route after its certificate is issued", loopruntimev1.ExposedRoutePhasePending, routeObservation{certReady: true, probesExhausted: true}, loopruntimev1.ExposedRoutePhaseFailed},
		{"a single failed probe keeps a Reachable route Reachable", loopruntimev1.ExposedRoutePhaseReachable, routeObservation{certReady: true}, loopruntimev1.ExposedRoutePhaseReachable},
		{"a Failed route with a failing probe stays Failed", loopruntimev1.ExposedRoutePhaseFailed, routeObservation{certReady: true, probesExhausted: true}, loopruntimev1.ExposedRoutePhaseFailed},
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
