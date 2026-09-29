// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package app

import (
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/conditions"
)

func routeWithIssuedCertificate() *loopzerov1.ExposedRoute {
	route := &loopzerov1.ExposedRoute{}
	route.Generation = 1
	conditions.SetCondition(&route.Status.Conditions, conditions.TypeCertificateIssued,
		metav1.ConditionTrue, conditions.ReasonReady, "certificate is ready", 1)
	return route
}

func conditionStatus(route *loopzerov1.ExposedRoute, conditionType string) metav1.ConditionStatus {
	for _, c := range route.Status.Conditions {
		if c.Type == conditionType {
			return c.Status
		}
	}
	return ""
}

// The route summary requires IngressReady, and nothing set it on a route: since
// 2026-05-04 no ExposedRoute ever reached Ready, even serving 200 through the edge
// (#447). The exposure controller does not own the ingress object — GitOps ships it —
// so what it can observe is the ingress serving the route: a successful probe.
func TestAReachableRouteWithItsCertificateIsReady(t *testing.T) {
	route := routeWithIssuedCertificate()
	r := &ExposureReconciler{}

	r.recordProbe(route, 200, nil)
	conditions.SetReadinessSummaryViaPkg(&route.Status.Conditions, route.Generation, exposureDetailTypes)

	if got := conditionStatus(route, conditions.TypeIngressReady); got != metav1.ConditionTrue {
		t.Errorf("IngressReady = %q after a 200 through the ingress, want True", got)
	}
	if got := conditionStatus(route, "Ready"); got != metav1.ConditionTrue {
		t.Errorf("Ready = %q for a reachable route with its certificate, want True", got)
	}
}

// A failed probe says nothing about the ingress alone — DNS, the edge or the app may
// be at fault — so IngressReady is not asserted True, and the route is not Ready.
func TestAnUnreachableRouteIsNotReady(t *testing.T) {
	route := routeWithIssuedCertificate()
	r := &ExposureReconciler{}

	r.recordProbe(route, 0, errors.New("dial tcp: connection refused"))
	conditions.SetReadinessSummaryViaPkg(&route.Status.Conditions, route.Generation, exposureDetailTypes)

	if got := conditionStatus(route, conditions.TypeIngressReady); got == metav1.ConditionTrue {
		t.Error("IngressReady is True after a failed probe")
	}
	if got := conditionStatus(route, "Ready"); got == metav1.ConditionTrue {
		t.Error("an unreachable route reports Ready")
	}
}
