// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package app

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/conditions"
)

func appIn(phase loopzerov1.ApplicationPhase, endpointsReady, reachable bool) *loopzerov1.LoopZeroApplication {
	return &loopzerov1.LoopZeroApplication{Status: loopzerov1.LoopZeroApplicationStatus{
		Phase: phase, EndpointsReady: endpointsReady, Reachable: reachable,
	}}
}

func withRouteReason(app *loopzerov1.LoopZeroApplication, reason string) *loopzerov1.LoopZeroApplication {
	conditions.SetCondition(&app.Status.Conditions, conditions.TypeRouteReachable,
		metav1.ConditionFalse, reason, "route probe", 1)
	return app
}

// Every phase change goes through the Application state machine: the phase an
// observation implies is reached only along declared transitions.
func TestAdvancePhase_FollowsTheDeclaredTransitions(t *testing.T) {
	cases := []struct {
		name string
		app  *loopzerov1.LoopZeroApplication
		want loopzerov1.ApplicationPhase
	}{
		{"a new application starts Progressing", appIn("", false, false), loopzerov1.ApplicationPhaseProgressing},
		{"ready endpoints move Progressing to Serving", appIn(loopzerov1.ApplicationPhaseProgressing, true, false), loopzerov1.ApplicationPhaseServing},
		{"a reachable route from Progressing passes through Serving to Reachable", appIn(loopzerov1.ApplicationPhaseProgressing, true, true), loopzerov1.ApplicationPhaseReachable},
		{"a new application already reachable reaches Reachable", appIn("", true, true), loopzerov1.ApplicationPhaseReachable},
		{"a route that stops answering degrades a Reachable application", appIn(loopzerov1.ApplicationPhaseReachable, true, false), loopzerov1.ApplicationPhaseDegraded},
		{"a recovered route brings Degraded back to Reachable", appIn(loopzerov1.ApplicationPhaseDegraded, true, true), loopzerov1.ApplicationPhaseReachable},
		{"a failed route takes a Serving application Down", withRouteReason(appIn(loopzerov1.ApplicationPhaseServing, true, false), conditions.ReasonFailed), loopzerov1.ApplicationPhaseDown},
		{"returning endpoints recover Down through Progressing to Serving", appIn(loopzerov1.ApplicationPhaseDown, true, false), loopzerov1.ApplicationPhaseServing},
		{"a Serving application waiting for its route stays Serving", appIn(loopzerov1.ApplicationPhaseServing, true, false), loopzerov1.ApplicationPhaseServing},
		{"a degraded signal cannot skip Reachable from Serving", withRouteReason(appIn(loopzerov1.ApplicationPhaseServing, true, false), conditions.ReasonDegraded), loopzerov1.ApplicationPhaseServing},
		{"a Cleaned application is terminal", appIn(loopzerov1.ApplicationPhaseCleaned, true, true), loopzerov1.ApplicationPhaseCleaned},
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
