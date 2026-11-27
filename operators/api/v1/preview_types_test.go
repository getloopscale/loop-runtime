// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package v1

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPreviewPhase_Values(t *testing.T) {
	phases := []PreviewPhase{
		PreviewPhaseCreating,
		PreviewPhaseActive,
		PreviewPhaseExpiring,
		PreviewPhaseDeleted,
		PreviewPhaseFailed,
		PreviewPhaseDeleting,
		PreviewPhaseCleanupFailed,
	}
	expected := []string{
		"Creating",
		"Active",
		"Expiring",
		"Deleted",
		"Failed",
		"Deleting",
		"CleanupFailed",
	}

	for i, p := range phases {
		if string(p) != expected[i] {
			t.Errorf("expected phase %q, got %q", expected[i], p)
		}
	}
}

func TestPreviewPhase_AreDistinct(t *testing.T) {
	phases := []PreviewPhase{
		PreviewPhaseCreating,
		PreviewPhaseActive,
		PreviewPhaseExpiring,
		PreviewPhaseDeleted,
		PreviewPhaseFailed,
		PreviewPhaseDeleting,
		PreviewPhaseCleanupFailed,
	}
	seen := make(map[PreviewPhase]bool)
	for _, p := range phases {
		if seen[p] {
			t.Errorf("duplicate preview phase: %q", p)
		}
		seen[p] = true
	}
}

func TestPreviewEnvironment_SpecFields(t *testing.T) {
	expiresAt := metav1.NewTime(time.Now().Add(24 * time.Hour))

	pe := PreviewEnvironment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "preview-pr-42",
			Namespace: "loop-runtime-system",
		},
		Spec: PreviewEnvironmentSpec{
			TenantID:  "tenant-abc",
			ProjectID: "project-xyz",
			PRNumber:  42,
			Branch:    "feat/new-feature",
			ExpiresAt: &expiresAt,
			ReleaseID: "rel-001",
		},
	}

	if pe.Spec.TenantID != "tenant-abc" {
		t.Errorf("expected TenantID %q, got %q", "tenant-abc", pe.Spec.TenantID)
	}
	if pe.Spec.ProjectID != "project-xyz" {
		t.Errorf("expected ProjectID %q, got %q", "project-xyz", pe.Spec.ProjectID)
	}
	if pe.Spec.PRNumber != 42 {
		t.Errorf("expected PRNumber %d, got %d", 42, pe.Spec.PRNumber)
	}
	if pe.Spec.Branch != "feat/new-feature" {
		t.Errorf("expected Branch %q, got %q", "feat/new-feature", pe.Spec.Branch)
	}
	if pe.Spec.ExpiresAt == nil {
		t.Fatal("expected ExpiresAt to be set")
	}
	if pe.Spec.ReleaseID != "rel-001" {
		t.Errorf("expected ReleaseID %q, got %q", "rel-001", pe.Spec.ReleaseID)
	}
}

func TestPreviewEnvironment_StatusFields(t *testing.T) {
	createdAt := metav1.Now()

	pe := PreviewEnvironment{
		Status: PreviewEnvironmentStatus{
			Phase:     PreviewPhaseActive,
			URL:       "https://pr-42.preview.example.com",
			CreatedAt: &createdAt,
		},
	}

	if pe.Status.Phase != PreviewPhaseActive {
		t.Errorf("expected phase %q, got %q", PreviewPhaseActive, pe.Status.Phase)
	}
	if pe.Status.URL != "https://pr-42.preview.example.com" {
		t.Errorf("expected URL %q, got %q", "https://pr-42.preview.example.com", pe.Status.URL)
	}
	if pe.Status.CreatedAt == nil {
		t.Fatal("expected CreatedAt to be set")
	}
}

func TestPreviewEnvironment_StatusConditions(t *testing.T) {
	pe := PreviewEnvironment{}

	SetCondition(&pe.Status.Conditions, "Ready", metav1.ConditionTrue, "Deployed", "preview is live", 1)
	SetCondition(&pe.Status.Conditions, "ResourcesReady", metav1.ConditionFalse, "Pending", "waiting for pods", 1)

	if len(pe.Status.Conditions) != 2 {
		t.Fatalf("expected 2 conditions, got %d", len(pe.Status.Conditions))
	}
	if pe.Status.Conditions[0].Type != "Ready" {
		t.Errorf("expected first condition type %q, got %q", "Ready", pe.Status.Conditions[0].Type)
	}
	if pe.Status.Conditions[1].Status != metav1.ConditionFalse {
		t.Errorf("expected second condition status False, got %v", pe.Status.Conditions[1].Status)
	}
}

func TestPreviewEnvironmentList_ContainsItems(t *testing.T) {
	list := PreviewEnvironmentList{
		Items: []PreviewEnvironment{
			{ObjectMeta: metav1.ObjectMeta{Name: "preview-1"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "preview-2"}},
		},
	}

	if len(list.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(list.Items))
	}
	if list.Items[0].Name != "preview-1" {
		t.Errorf("expected first item name %q, got %q", "preview-1", list.Items[0].Name)
	}
}
