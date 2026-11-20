// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package v1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestApplicationPhase_Values(t *testing.T) {
	phases := []ApplicationPhase{
		ApplicationPhaseProgressing,
		ApplicationPhaseServing,
		ApplicationPhaseReachable,
		ApplicationPhaseDegraded,
		ApplicationPhaseDown,
	}
	expected := []string{"Progressing", "Serving", "Reachable", "Degraded", "Down"}

	for i, p := range phases {
		if string(p) != expected[i] {
			t.Errorf("expected phase %q, got %q", expected[i], p)
		}
	}
}

func TestApplicationPhase_AreDistinct(t *testing.T) {
	phases := []ApplicationPhase{
		ApplicationPhaseProgressing,
		ApplicationPhaseServing,
		ApplicationPhaseReachable,
		ApplicationPhaseDegraded,
		ApplicationPhaseDown,
	}
	seen := make(map[ApplicationPhase]bool)
	for _, p := range phases {
		if seen[p] {
			t.Errorf("duplicate application phase: %q", p)
		}
		seen[p] = true
	}
}

func TestLoopZeroApplication_SpecFields(t *testing.T) {
	app := LoopZeroApplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-app",
			Namespace: "loopzero-acme-production",
		},
		Spec: LoopZeroApplicationSpec{
			TenantRef:   "acme",
			Project:     "web",
			Environment: "production",
			ServiceName: "web-svc",
			Hostname:    "web.acme.dev.getloopscale.com",
			Replicas:    2,
			ServicePort: 8080,
		},
	}

	if app.Spec.TenantRef != "acme" {
		t.Errorf("expected TenantRef %q, got %q", "acme", app.Spec.TenantRef)
	}
	if app.Spec.Project != "web" {
		t.Errorf("expected Project %q, got %q", "web", app.Spec.Project)
	}
	if app.Spec.Environment != "production" {
		t.Errorf("expected Environment %q, got %q", "production", app.Spec.Environment)
	}
	if app.Spec.ServiceName != "web-svc" {
		t.Errorf("expected ServiceName %q, got %q", "web-svc", app.Spec.ServiceName)
	}
	if app.Spec.Hostname != "web.acme.dev.getloopscale.com" {
		t.Errorf("expected Hostname %q, got %q", "web.acme.dev.getloopscale.com", app.Spec.Hostname)
	}
	if app.Spec.Replicas != 2 {
		t.Errorf("expected Replicas %d, got %d", 2, app.Spec.Replicas)
	}
	if app.Spec.ServicePort != 8080 {
		t.Errorf("expected ServicePort %d, got %d", 8080, app.Spec.ServicePort)
	}
}

func TestLoopZeroApplication_SpecDefaults(t *testing.T) {
	app := LoopZeroApplication{
		Spec: LoopZeroApplicationSpec{
			TenantRef:   "acme",
			Project:     "api",
			Environment: "staging",
			ServiceName: "api-svc",
		},
	}

	if app.Spec.Hostname != "" {
		t.Errorf("expected empty Hostname by default, got %q", app.Spec.Hostname)
	}
	if app.Spec.Replicas != 0 {
		t.Errorf("expected zero-value Replicas before defaulting, got %d", app.Spec.Replicas)
	}
	if app.Spec.ServicePort != 0 {
		t.Errorf("expected zero-value ServicePort before defaulting, got %d", app.Spec.ServicePort)
	}
}

func TestLoopZeroApplication_StatusFields(t *testing.T) {
	app := LoopZeroApplication{
		Status: LoopZeroApplicationStatus{
			Phase:              ApplicationPhaseReachable,
			EndpointsReady:     true,
			Reachable:          true,
			URL:                "https://web.acme.dev.getloopscale.com",
			ObservedGeneration: 3,
		},
	}

	if app.Status.Phase != ApplicationPhaseReachable {
		t.Errorf("expected Phase %q, got %q", ApplicationPhaseReachable, app.Status.Phase)
	}
	if !app.Status.EndpointsReady {
		t.Error("expected EndpointsReady to be true")
	}
	if !app.Status.Reachable {
		t.Error("expected Reachable to be true")
	}
	if app.Status.URL != "https://web.acme.dev.getloopscale.com" {
		t.Errorf("expected URL %q, got %q", "https://web.acme.dev.getloopscale.com", app.Status.URL)
	}
	if app.Status.ObservedGeneration != 3 {
		t.Errorf("expected ObservedGeneration %d, got %d", 3, app.Status.ObservedGeneration)
	}
}

func TestLoopZeroApplication_SetCondition(t *testing.T) {
	app := LoopZeroApplication{}

	SetCondition(&app.Status.Conditions, TypeServiceResolved, metav1.ConditionTrue, "Resolved", "service found", 1)

	if len(app.Status.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(app.Status.Conditions))
	}
	if app.Status.Conditions[0].Type != TypeServiceResolved {
		t.Errorf("expected condition type %q, got %q", TypeServiceResolved, app.Status.Conditions[0].Type)
	}
	if app.Status.Conditions[0].Status != metav1.ConditionTrue {
		t.Errorf("expected condition status True, got %v", app.Status.Conditions[0].Status)
	}
}

func TestLoopZeroApplication_GetCondition(t *testing.T) {
	app := LoopZeroApplication{}

	SetCondition(&app.Status.Conditions, TypeServiceResolved, metav1.ConditionTrue, "Resolved", "ok", 1)
	SetCondition(&app.Status.Conditions, TypeIngressReady, metav1.ConditionFalse, "Pending", "creating", 1)

	c := GetCondition(app.Status.Conditions, TypeIngressReady)
	if c == nil {
		t.Fatal("expected to find IngressReady condition")
	}
	if c.Status != metav1.ConditionFalse {
		t.Errorf("expected condition status False, got %v", c.Status)
	}

	missing := GetCondition(app.Status.Conditions, "NonExistent")
	if missing != nil {
		t.Error("expected nil for non-existent condition")
	}
}

func TestLoopZeroApplication_ConditionTypes_AreDistinct(t *testing.T) {
	types := []string{
		TypeServiceResolved,
		TypeIngressReady,
		TypeCertificateIssued,
		TypeRouteReachable,
		TypeApplicationHealthy,
	}
	seen := make(map[string]bool)
	for _, ct := range types {
		if seen[ct] {
			t.Errorf("duplicate condition type: %q", ct)
		}
		seen[ct] = true
	}
}

func TestLoopZeroApplicationList_ContainsItems(t *testing.T) {
	list := LoopZeroApplicationList{
		Items: []LoopZeroApplication{
			{ObjectMeta: metav1.ObjectMeta{Name: "app-1"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "app-2"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "app-3"}},
		},
	}

	if len(list.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(list.Items))
	}
	if list.Items[0].Name != "app-1" {
		t.Errorf("expected first item name %q, got %q", "app-1", list.Items[0].Name)
	}
	if list.Items[2].Name != "app-3" {
		t.Errorf("expected third item name %q, got %q", "app-3", list.Items[2].Name)
	}
}
