// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package app

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/conditions"
)

func appIn(phase loopruntimev1.ApplicationPhase, endpointsReady, reachable bool) *loopruntimev1.LoopRuntimeApplication {
	return &loopruntimev1.LoopRuntimeApplication{Status: loopruntimev1.LoopRuntimeApplicationStatus{
		Phase: phase, EndpointsReady: endpointsReady, Reachable: reachable,
	}}
}

func withRouteReason(app *loopruntimev1.LoopRuntimeApplication, reason string) *loopruntimev1.LoopRuntimeApplication {
	conditions.SetCondition(&app.Status.Conditions, conditions.TypeRouteReachable,
		metav1.ConditionFalse, reason, "route probe", 1)
	return app
}

// Every phase change goes through the Application state machine: the phase an
// observation implies is reached only along declared transitions.
func TestAdvancePhase_FollowsTheDeclaredTransitions(t *testing.T) {
	cases := []struct {
		name string
		app  *loopruntimev1.LoopRuntimeApplication
		want loopruntimev1.ApplicationPhase
	}{
		{"a new application starts Progressing", appIn("", false, false), loopruntimev1.ApplicationPhaseProgressing},
		{"ready endpoints move Progressing to Serving", appIn(loopruntimev1.ApplicationPhaseProgressing, true, false), loopruntimev1.ApplicationPhaseServing},
		{"a reachable route from Progressing passes through Serving to Reachable", appIn(loopruntimev1.ApplicationPhaseProgressing, true, true), loopruntimev1.ApplicationPhaseReachable},
		{"a new application already reachable reaches Reachable", appIn("", true, true), loopruntimev1.ApplicationPhaseReachable},
		{"a route that stops answering degrades a Reachable application", appIn(loopruntimev1.ApplicationPhaseReachable, true, false), loopruntimev1.ApplicationPhaseDegraded},
		{"a recovered route brings Degraded back to Reachable", appIn(loopruntimev1.ApplicationPhaseDegraded, true, true), loopruntimev1.ApplicationPhaseReachable},
		{"a failed route takes a Serving application Down", withRouteReason(appIn(loopruntimev1.ApplicationPhaseServing, true, false), conditions.ReasonFailed), loopruntimev1.ApplicationPhaseDown},
		{"returning endpoints recover Down through Progressing to Serving", appIn(loopruntimev1.ApplicationPhaseDown, true, false), loopruntimev1.ApplicationPhaseServing},
		{"a Serving application waiting for its route stays Serving", appIn(loopruntimev1.ApplicationPhaseServing, true, false), loopruntimev1.ApplicationPhaseServing},
		{"a degraded signal cannot skip Reachable from Serving", withRouteReason(appIn(loopruntimev1.ApplicationPhaseServing, true, false), conditions.ReasonDegraded), loopruntimev1.ApplicationPhaseServing},
		{"a Cleaned application is terminal", appIn(loopruntimev1.ApplicationPhaseCleaned, true, true), loopruntimev1.ApplicationPhaseCleaned},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := advancePhase(tc.app)
			if err != nil {
				t.Fatalf("advancePhase: %v", err)
			}
			if got != tc.want {
				t.Fatalf("phase = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAdvancePhase_RejectsAPhaseTheMachineDoesNotKnow(t *testing.T) {
	if _, err := advancePhase(appIn("Exploded", true, true)); err == nil {
		t.Fatal("an undeclared phase must be an error, not silently overwritten")
	}
}
