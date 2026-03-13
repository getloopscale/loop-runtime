package v1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSetCondition_AddsNewCondition(t *testing.T) {
	var conditions []metav1.Condition

	SetCondition(&conditions, ConditionGitRepoReady, metav1.ConditionTrue, "Created", "Git repository created")

	if len(conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conditions))
	}
	if conditions[0].Type != ConditionGitRepoReady {
		t.Errorf("expected type %q, got %q", ConditionGitRepoReady, conditions[0].Type)
	}
	if conditions[0].Status != metav1.ConditionTrue {
		t.Errorf("expected status True, got %v", conditions[0].Status)
	}
	if conditions[0].Reason != "Created" {
		t.Errorf("expected reason %q, got %q", "Created", conditions[0].Reason)
	}
}

func TestSetCondition_UpdatesExistingConditionSameStatus(t *testing.T) {
	conditions := []metav1.Condition{
		{
			Type:               ConditionGitRepoReady,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "Created",
			Message:            "old message",
		},
	}
	originalTime := conditions[0].LastTransitionTime

	SetCondition(&conditions, ConditionGitRepoReady, metav1.ConditionTrue, "Created", "new message")

	if len(conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conditions))
	}
	// LastTransitionTime should NOT change when status is the same.
	if !conditions[0].LastTransitionTime.Equal(&originalTime) {
		t.Error("LastTransitionTime should not change when status stays the same")
	}
	if conditions[0].Message != "new message" {
		t.Errorf("expected message %q, got %q", "new message", conditions[0].Message)
	}
}

func TestSetCondition_UpdatesExistingConditionDifferentStatus(t *testing.T) {
	conditions := []metav1.Condition{
		{
			Type:               ConditionGitRepoReady,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             "Pending",
			Message:            "waiting",
		},
	}

	SetCondition(&conditions, ConditionGitRepoReady, metav1.ConditionTrue, "Created", "done")

	if len(conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conditions))
	}
	if conditions[0].Status != metav1.ConditionTrue {
		t.Errorf("expected status True, got %v", conditions[0].Status)
	}
	if conditions[0].Reason != "Created" {
		t.Errorf("expected reason %q, got %q", "Created", conditions[0].Reason)
	}
}

func TestSetCondition_MultipleConditions(t *testing.T) {
	var conditions []metav1.Condition

	SetCondition(&conditions, ConditionGitRepoReady, metav1.ConditionTrue, "Created", "done")
	SetCondition(&conditions, ConditionNamespacesReady, metav1.ConditionFalse, "Pending", "creating")
	SetCondition(&conditions, ConditionRBACReady, metav1.ConditionTrue, "Applied", "done")

	if len(conditions) != 3 {
		t.Fatalf("expected 3 conditions, got %d", len(conditions))
	}
}

func TestTenantPhase_Values(t *testing.T) {
	phases := []TenantPhase{
		TenantPhaseProvisioning,
		TenantPhaseActive,
		TenantPhaseSuspended,
		TenantPhaseDeleting,
	}
	expected := []string{"Provisioning", "Active", "Suspended", "Deleting"}

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
	if GroupVersion.Group != "loopzero.io" {
		t.Errorf("expected group %q, got %q", "loopzero.io", GroupVersion.Group)
	}
	if GroupVersion.Version != "v1" {
		t.Errorf("expected version %q, got %q", "v1", GroupVersion.Version)
	}
}

func TestResource(t *testing.T) {
	gr := Resource("tenants")
	if gr.Group != "loopzero.io" {
		t.Errorf("expected group %q, got %q", "loopzero.io", gr.Group)
	}
	if gr.Resource != "tenants" {
		t.Errorf("expected resource %q, got %q", "tenants", gr.Resource)
	}
}

func TestConditionTypes_AreDistinct(t *testing.T) {
	types := []string{
		ConditionGitRepoReady,
		ConditionNamespacesReady,
		ConditionRBACReady,
		ConditionQuotaEnforced,
		ConditionNetworkPolicyReady,
		ConditionBuildNamespaceReady,
	}
	seen := make(map[string]bool)
	for _, ct := range types {
		if seen[ct] {
			t.Errorf("duplicate condition type: %q", ct)
		}
		seen[ct] = true
	}
}
