// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package v1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSetCondition_AddsNewCondition(t *testing.T) {
	var conds []metav1.Condition

	SetCondition(&conds, TypeGitRepoReady, metav1.ConditionTrue, "Created", "Git repository created", 1)

	if len(conds) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conds))
	}
	if conds[0].Type != TypeGitRepoReady {
		t.Errorf("expected type %q, got %q", TypeGitRepoReady, conds[0].Type)
	}
	if conds[0].Status != metav1.ConditionTrue {
		t.Errorf("expected status True, got %v", conds[0].Status)
	}
	if conds[0].Reason != "Created" {
		t.Errorf("expected reason %q, got %q", "Created", conds[0].Reason)
	}
}

func TestSetCondition_UpdatesExistingConditionSameStatus(t *testing.T) {
	conds := []metav1.Condition{
		{
			Type:               TypeGitRepoReady,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "Created",
			Message:            "old message",
		},
	}
	originalTime := conds[0].LastTransitionTime

	SetCondition(&conds, TypeGitRepoReady, metav1.ConditionTrue, "Created", "new message", 1)

	if len(conds) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conds))
	}
	// LastTransitionTime should NOT change when status is the same.
	if !conds[0].LastTransitionTime.Equal(&originalTime) {
		t.Error("LastTransitionTime should not change when status stays the same")
	}
	if conds[0].Message != "new message" {
		t.Errorf("expected message %q, got %q", "new message", conds[0].Message)
	}
}

func TestSetCondition_UpdatesExistingConditionDifferentStatus(t *testing.T) {
	conds := []metav1.Condition{
		{
			Type:               TypeGitRepoReady,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "Pending",
			Message:            "waiting",
		},
	}

	SetCondition(&conds, TypeGitRepoReady, metav1.ConditionTrue, "Created", "done", 1)

	if len(conds) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conds))
	}
	if conds[0].Status != metav1.ConditionTrue {
		t.Errorf("expected status True, got %v", conds[0].Status)
	}
	if conds[0].Reason != "Created" {
		t.Errorf("expected reason %q, got %q", "Created", conds[0].Reason)
	}
}

func TestSetCondition_MultipleConditions(t *testing.T) {
	var conds []metav1.Condition

	SetCondition(&conds, TypeGitRepoReady, metav1.ConditionTrue, "Created", "done", 1)
	SetCondition(&conds, TypeBuildNamespaceReady, metav1.ConditionFalse, "Pending", "creating", 1)
	SetCondition(&conds, TypeQuotaEnforced, metav1.ConditionTrue, "Applied", "done", 1)

	if len(conds) != 3 {
		t.Fatalf("expected 3 conditions, got %d", len(conds))
	}
}

func TestTenantPhase_Values(t *testing.T) {
	phases := []TenantPhase{
		TenantPhaseProvisioning,
		TenantPhaseActive,
		TenantPhaseSuspended,
		TenantPhasePendingDeletion,
		TenantPhaseDeleting,
	}
	expected := []string{"Provisioning", "Active", "Suspended", "PendingDeletion", "Deleting"}

	for i, p := range phases {
		if string(p) != expected[i] {
			t.Errorf("expected phase %q, got %q", expected[i], p)
		}
	}
}

func TestPlanTier_Values(t *testing.T) {
	plans := []PlanTier{PlanStarter, PlanGrowth, PlanEnterprise}
	expected := []string{"starter", "growth", "enterprise"}

	for i, p := range plans {
		if string(p) != expected[i] {
			t.Errorf("expected plan %q, got %q", expected[i], p)
		}
	}
}

func TestGroupVersion(t *testing.T) {
	if GroupVersion.Group != "runtime.getloopscale.com" {
		t.Errorf("expected group %q, got %q", "runtime.getloopscale.com", GroupVersion.Group)
	}
	if GroupVersion.Version != "v1" {
		t.Errorf("expected version %q, got %q", "v1", GroupVersion.Version)
	}
}

func TestResource(t *testing.T) {
	gr := Resource("tenants")
	if gr.Group != "runtime.getloopscale.com" {
		t.Errorf("expected group %q, got %q", "runtime.getloopscale.com", gr.Group)
	}
	if gr.Resource != "tenants" {
		t.Errorf("expected resource %q, got %q", "tenants", gr.Resource)
	}
}

func TestConditionTypes_AreDistinct(t *testing.T) {
	types := []string{
		TypeGitRepoReady,
		TypeQuotaEnforced,
		TypeBuildNamespaceReady,
		TypePriorityClassReady,
		TypeArgoCDAppReady,
	}
	seen := make(map[string]bool)
	for _, ct := range types {
		if seen[ct] {
			t.Errorf("duplicate condition type: %q", ct)
		}
		seen[ct] = true
	}
}
