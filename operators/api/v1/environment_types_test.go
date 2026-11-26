// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package v1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEnvironmentPhase_Values(t *testing.T) {
	phases := []EnvironmentPhase{
		EnvironmentPhasePending,
		EnvironmentPhaseActive,
		EnvironmentPhaseDeleting,
		EnvironmentPhaseFailed,
	}
	expected := []string{"Pending", "Active", "Deleting", "Failed"}

	for i, p := range phases {
		if string(p) != expected[i] {
			t.Errorf("expected phase %q, got %q", expected[i], p)
		}
	}
}

func TestEnvironmentPhase_AreDistinct(t *testing.T) {
	phases := []EnvironmentPhase{
		EnvironmentPhasePending,
		EnvironmentPhaseActive,
		EnvironmentPhaseDeleting,
		EnvironmentPhaseFailed,
	}
	seen := make(map[EnvironmentPhase]bool)
	for _, p := range phases {
		if seen[p] {
			t.Errorf("duplicate environment phase: %q", p)
		}
		seen[p] = true
	}
}

func TestEnvironment_SpecFields(t *testing.T) {
	env := Environment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "acme-production",
			Namespace: "loopruntime-system",
		},
		Spec: EnvironmentCRSpec{
			TenantRef:   TenantRef{Name: "acme"},
			Environment: "production",
			Namespace:   NamespaceSpec{Name: "loopruntime-acme-production"},
			ResourceQuota: &ResourceQuotaSpec{
				CPU:     "8",
				Memory:  "16Gi",
				Storage: "50Gi",
			},
			NetworkPolicy: NetworkPolicyConfig{Isolate: true},
			Plan:          PlanStarter,
		},
	}

	if env.Spec.TenantRef.Name != "acme" {
		t.Errorf("expected TenantRef.Name %q, got %q", "acme", env.Spec.TenantRef.Name)
	}
	if env.Spec.Environment != "production" {
		t.Errorf("expected Environment %q, got %q", "production", env.Spec.Environment)
	}
	if env.Spec.Namespace.Name != "loopruntime-acme-production" {
		t.Errorf("expected Namespace.Name %q, got %q", "loopruntime-acme-production", env.Spec.Namespace.Name)
	}
	if env.Spec.ResourceQuota == nil {
		t.Fatal("expected ResourceQuota to be set")
	}
	if env.Spec.ResourceQuota.CPU != "8" {
		t.Errorf("expected ResourceQuota.CPU %q, got %q", "8", env.Spec.ResourceQuota.CPU)
	}
	if env.Spec.ResourceQuota.Memory != "16Gi" {
		t.Errorf("expected ResourceQuota.Memory %q, got %q", "16Gi", env.Spec.ResourceQuota.Memory)
	}
	if env.Spec.ResourceQuota.Storage != "50Gi" {
		t.Errorf("expected ResourceQuota.Storage %q, got %q", "50Gi", env.Spec.ResourceQuota.Storage)
	}
	if !env.Spec.NetworkPolicy.Isolate {
		t.Error("expected NetworkPolicy.Isolate to be true")
	}
	if env.Spec.Plan != PlanStarter {
		t.Errorf("expected Plan %q, got %q", PlanStarter, env.Spec.Plan)
	}
}

func TestEnvironment_SpecWithoutResourceQuota(t *testing.T) {
	env := Environment{
		Spec: EnvironmentCRSpec{
			TenantRef:     TenantRef{Name: "acme"},
			Environment:   "staging",
			Namespace:     NamespaceSpec{Name: "loopruntime-acme-staging"},
			NetworkPolicy: NetworkPolicyConfig{Isolate: false},
			Plan:          PlanGrowth,
		},
	}

	if env.Spec.ResourceQuota != nil {
		t.Error("expected ResourceQuota to be nil when not set")
	}
	if env.Spec.NetworkPolicy.Isolate {
		t.Error("expected NetworkPolicy.Isolate to be false")
	}
}

func TestEnvironment_StatusFields(t *testing.T) {
	env := Environment{
		Status: EnvironmentCRStatus{
			Phase:              EnvironmentPhaseActive,
			Namespace:          "loopruntime-acme-production",
			ObservedGeneration: 5,
		},
	}

	if env.Status.Phase != EnvironmentPhaseActive {
		t.Errorf("expected Phase %q, got %q", EnvironmentPhaseActive, env.Status.Phase)
	}
	if env.Status.Namespace != "loopruntime-acme-production" {
		t.Errorf("expected Namespace %q, got %q", "loopruntime-acme-production", env.Status.Namespace)
	}
	if env.Status.ObservedGeneration != 5 {
		t.Errorf("expected ObservedGeneration %d, got %d", 5, env.Status.ObservedGeneration)
	}
}

func TestEnvironment_SetCondition(t *testing.T) {
	env := Environment{}

	SetCondition(&env.Status.Conditions, TypeEnvNamespaceReady, metav1.ConditionTrue, "Created", "namespace created", 1)
	SetCondition(&env.Status.Conditions, TypeEnvRBACReady, metav1.ConditionFalse, "Pending", "creating RBAC", 1)

	if len(env.Status.Conditions) != 2 {
		t.Fatalf("expected 2 conditions, got %d", len(env.Status.Conditions))
	}
}

func TestEnvironment_GetCondition(t *testing.T) {
	env := Environment{}

	SetCondition(&env.Status.Conditions, TypeEnvNamespaceReady, metav1.ConditionTrue, "Created", "ok", 1)

	c := GetCondition(env.Status.Conditions, TypeEnvNamespaceReady)
	if c == nil {
		t.Fatal("expected to find NamespaceReady condition")
	}
	if c.Status != metav1.ConditionTrue {
		t.Errorf("expected condition status True, got %v", c.Status)
	}

	missing := GetCondition(env.Status.Conditions, "NonExistent")
	if missing != nil {
		t.Error("expected nil for non-existent condition")
	}
}

func TestEnvironment_ConditionTypes_AreDistinct(t *testing.T) {
	types := []string{
		TypeEnvNamespaceReady,
		TypeEnvRBACReady,
		TypeEnvNetworkPolicyReady,
		TypeEnvQuotaReady,
		TypePullSecretReady,
	}
	seen := make(map[string]bool)
	for _, ct := range types {
		if seen[ct] {
			t.Errorf("duplicate condition type: %q", ct)
		}
		seen[ct] = true
	}
}

func TestEnvironmentList_ContainsItems(t *testing.T) {
	list := EnvironmentList{
		Items: []Environment{
			{ObjectMeta: metav1.ObjectMeta{Name: "env-prod"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "env-staging"}},
		},
	}

	if len(list.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(list.Items))
	}
	if list.Items[0].Name != "env-prod" {
		t.Errorf("expected first item name %q, got %q", "env-prod", list.Items[0].Name)
	}
	if list.Items[1].Name != "env-staging" {
		t.Errorf("expected second item name %q, got %q", "env-staging", list.Items[1].Name)
	}
}
