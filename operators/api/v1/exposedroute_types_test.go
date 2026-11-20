// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package v1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestExposedRoutePhase_Values(t *testing.T) {
	phases := []ExposedRoutePhase{
		ExposedRoutePhasePending,
		ExposedRoutePhaseCertIssued,
		ExposedRoutePhaseReachable,
		ExposedRoutePhaseFailed,
	}
	expected := []string{"Pending", "CertIssued", "Reachable", "Failed"}

	for i, p := range phases {
		if string(p) != expected[i] {
			t.Errorf("expected phase %q, got %q", expected[i], p)
		}
	}
}

func TestExposedRoutePhase_AreDistinct(t *testing.T) {
	phases := []ExposedRoutePhase{
		ExposedRoutePhasePending,
		ExposedRoutePhaseCertIssued,
		ExposedRoutePhaseReachable,
		ExposedRoutePhaseFailed,
	}
	seen := make(map[ExposedRoutePhase]bool)
	for _, p := range phases {
		if seen[p] {
			t.Errorf("duplicate exposed route phase: %q", p)
		}
		seen[p] = true
	}
}

func TestExposedRoute_SpecFields(t *testing.T) {
	route := ExposedRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-route",
			Namespace: "loopzero-acme-production",
		},
		Spec: ExposedRouteSpec{
			ApplicationRef: "my-app",
			Hostname:       "web.acme.dev.getloopscale.com",
			ServiceRef: ServiceRef{
				Name: "web-svc",
				Port: 8080,
			},
			TLS: TLSConfig{
				Enabled:   true,
				IssuerRef: "letsencrypt-prod",
			},
		},
	}

	if route.Spec.ApplicationRef != "my-app" {
		t.Errorf("expected ApplicationRef %q, got %q", "my-app", route.Spec.ApplicationRef)
	}
	if route.Spec.Hostname != "web.acme.dev.getloopscale.com" {
		t.Errorf("expected Hostname %q, got %q", "web.acme.dev.getloopscale.com", route.Spec.Hostname)
	}
	if route.Spec.ServiceRef.Name != "web-svc" {
		t.Errorf("expected ServiceRef.Name %q, got %q", "web-svc", route.Spec.ServiceRef.Name)
	}
	if route.Spec.ServiceRef.Port != 8080 {
		t.Errorf("expected ServiceRef.Port %d, got %d", 8080, route.Spec.ServiceRef.Port)
	}
	if !route.Spec.TLS.Enabled {
		t.Error("expected TLS.Enabled to be true")
	}
	if route.Spec.TLS.IssuerRef != "letsencrypt-prod" {
		t.Errorf("expected TLS.IssuerRef %q, got %q", "letsencrypt-prod", route.Spec.TLS.IssuerRef)
	}
}

func TestExposedRoute_SpecTLSDisabled(t *testing.T) {
	route := ExposedRoute{
		Spec: ExposedRouteSpec{
			ApplicationRef: "my-app",
			Hostname:       "internal.acme.local",
			ServiceRef:     ServiceRef{Name: "internal-svc", Port: 80},
		},
	}

	if route.Spec.TLS.Enabled {
		t.Error("expected TLS.Enabled to default to false")
	}
	if route.Spec.TLS.IssuerRef != "" {
		t.Errorf("expected empty IssuerRef by default, got %q", route.Spec.TLS.IssuerRef)
	}
}

func TestExposedRoute_StatusFields(t *testing.T) {
	probeTime := metav1.Now()

	route := ExposedRoute{
		Status: ExposedRouteStatus{
			Phase:               ExposedRoutePhaseReachable,
			CertificateReady:    true,
			LastProbeTime:       &probeTime,
			LastProbeResult:     200,
			ConsecutiveFailures: 0,
			ObservedGeneration:  2,
		},
	}

	if route.Status.Phase != ExposedRoutePhaseReachable {
		t.Errorf("expected Phase %q, got %q", ExposedRoutePhaseReachable, route.Status.Phase)
	}
	if !route.Status.CertificateReady {
		t.Error("expected CertificateReady to be true")
	}
	if route.Status.LastProbeTime == nil {
		t.Fatal("expected LastProbeTime to be set")
	}
	if route.Status.LastProbeResult != 200 {
		t.Errorf("expected LastProbeResult %d, got %d", 200, route.Status.LastProbeResult)
	}
	if route.Status.ConsecutiveFailures != 0 {
		t.Errorf("expected ConsecutiveFailures %d, got %d", 0, route.Status.ConsecutiveFailures)
	}
	if route.Status.ObservedGeneration != 2 {
		t.Errorf("expected ObservedGeneration %d, got %d", 2, route.Status.ObservedGeneration)
	}
}

func TestExposedRoute_StatusDegraded(t *testing.T) {
	route := ExposedRoute{
		Status: ExposedRouteStatus{
			Phase:               ExposedRoutePhaseFailed,
			CertificateReady:    false,
			ConsecutiveFailures: 5,
			LastProbeResult:     503,
		},
	}

	if route.Status.Phase != ExposedRoutePhaseFailed {
		t.Errorf("expected Phase %q, got %q", ExposedRoutePhaseFailed, route.Status.Phase)
	}
	if route.Status.CertificateReady {
		t.Error("expected CertificateReady to be false")
	}
	if route.Status.ConsecutiveFailures != 5 {
		t.Errorf("expected ConsecutiveFailures %d, got %d", 5, route.Status.ConsecutiveFailures)
	}
}

func TestExposedRoute_SetCondition(t *testing.T) {
	route := ExposedRoute{}

	SetCondition(&route.Status.Conditions, TypeCertificateIssued, metav1.ConditionTrue, "Issued", "certificate ready", 1)
	SetCondition(&route.Status.Conditions, TypeRouteReachable, metav1.ConditionFalse, "Probing", "waiting for probe", 1)

	if len(route.Status.Conditions) != 2 {
		t.Fatalf("expected 2 conditions, got %d", len(route.Status.Conditions))
	}

	c := GetCondition(route.Status.Conditions, TypeCertificateIssued)
	if c == nil {
		t.Fatal("expected to find CertificateIssued condition")
	}
	if c.Status != metav1.ConditionTrue {
		t.Errorf("expected condition status True, got %v", c.Status)
	}
}

func TestExposedRoute_ConditionTypes_AreDistinct(t *testing.T) {
	types := []string{
		TypeCertificateIssued,
		TypeRouteReachable,
		TypeServiceResolved,
		TypeIngressReady,
	}
	seen := make(map[string]bool)
	for _, ct := range types {
		if seen[ct] {
			t.Errorf("duplicate condition type: %q", ct)
		}
		seen[ct] = true
	}
}

func TestExposedRouteList_ContainsItems(t *testing.T) {
	list := ExposedRouteList{
		Items: []ExposedRoute{
			{ObjectMeta: metav1.ObjectMeta{Name: "route-1"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "route-2"}},
		},
	}

	if len(list.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(list.Items))
	}
	if list.Items[0].Name != "route-1" {
		t.Errorf("expected first item name %q, got %q", "route-1", list.Items[0].Name)
	}
	if list.Items[1].Name != "route-2" {
		t.Errorf("expected second item name %q, got %q", "route-2", list.Items[1].Name)
	}
}
