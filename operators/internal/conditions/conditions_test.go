// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package conditions

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSetCondition_CreatesNewCondition(t *testing.T) {
	var conditions []metav1.Condition

	SetCondition(&conditions, TypeGitRepoReady, metav1.ConditionTrue, ReasonReady, "repo is ready", 1)

	if len(conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conditions))
	}
	c := conditions[0]
	if c.Type != TypeGitRepoReady {
		t.Errorf("expected type %q, got %q", TypeGitRepoReady, c.Type)
	}
	if c.Status != metav1.ConditionTrue {
		t.Errorf("expected status True, got %s", c.Status)
	}
	if c.Reason != ReasonReady {
		t.Errorf("expected reason %q, got %q", ReasonReady, c.Reason)
	}
	if c.Message != "repo is ready" {
		t.Errorf("expected message %q, got %q", "repo is ready", c.Message)
	}
	if c.ObservedGeneration != 1 {
		t.Errorf("expected observedGeneration 1, got %d", c.ObservedGeneration)
	}
	if c.LastTransitionTime.IsZero() {
		t.Error("expected LastTransitionTime to be set, got zero")
	}
}

func TestSetCondition_PreservesLastTransitionTimeWhenStatusUnchanged(t *testing.T) {
	fixedTime := metav1.NewTime(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	conditions := []metav1.Condition{
		{
			Type:               TypeGitRepoReady,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: fixedTime,
			Reason:             ReasonReady,
			Message:            "old message",
			ObservedGeneration: 1,
		},
	}

	SetCondition(&conditions, TypeGitRepoReady, metav1.ConditionTrue, ReasonReconciling, "new message", 2)

	if len(conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conditions))
	}
	c := conditions[0]
	if !c.LastTransitionTime.Equal(&fixedTime) {
		t.Errorf("expected LastTransitionTime to be preserved (%v), got %v", fixedTime, c.LastTransitionTime)
	}
	if c.Reason != ReasonReconciling {
		t.Errorf("expected reason %q, got %q", ReasonReconciling, c.Reason)
	}
	if c.Message != "new message" {
		t.Errorf("expected message %q, got %q", "new message", c.Message)
	}
	if c.ObservedGeneration != 2 {
		t.Errorf("expected observedGeneration 2, got %d", c.ObservedGeneration)
	}
}

func TestSetCondition_UpdatesLastTransitionTimeWhenStatusChanges(t *testing.T) {
	fixedTime := metav1.NewTime(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	conditions := []metav1.Condition{
		{
			Type:               TypeGitRepoReady,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: fixedTime,
			Reason:             ReasonReconciling,
			Message:            "not ready",
			ObservedGeneration: 1,
		},
	}

	SetCondition(&conditions, TypeGitRepoReady, metav1.ConditionTrue, ReasonReady, "ready now", 2)

	c := conditions[0]
	if c.LastTransitionTime.Equal(&fixedTime) {
		t.Error("expected LastTransitionTime to be updated when status changes, but it was preserved")
	}
	if c.Status != metav1.ConditionTrue {
		t.Errorf("expected status True, got %s", c.Status)
	}
}

func TestSetCondition_AlwaysSetsObservedGeneration(t *testing.T) {
	var conditions []metav1.Condition

	SetCondition(&conditions, TypeGitRepoReady, metav1.ConditionTrue, ReasonReady, "ready", 42)

	if conditions[0].ObservedGeneration != 42 {
		t.Errorf("expected observedGeneration 42, got %d", conditions[0].ObservedGeneration)
	}

	// Update with same status, different generation
	SetCondition(&conditions, TypeGitRepoReady, metav1.ConditionTrue, ReasonReady, "ready", 43)

	if conditions[0].ObservedGeneration != 43 {
		t.Errorf("expected observedGeneration 43 after update, got %d", conditions[0].ObservedGeneration)
	}

	// Update with different status, different generation
	SetCondition(&conditions, TypeGitRepoReady, metav1.ConditionFalse, ReasonFailed, "failed", 44)

	if conditions[0].ObservedGeneration != 44 {
		t.Errorf("expected observedGeneration 44 after status change, got %d", conditions[0].ObservedGeneration)
	}
}

func TestGetCondition_ReturnsNilForMissingType(t *testing.T) {
	conditions := []metav1.Condition{
		{Type: TypeGitRepoReady, Status: metav1.ConditionTrue},
	}

	result := GetCondition(conditions, TypeBuildNamespaceReady)

	if result != nil {
		t.Errorf("expected nil for missing condition type, got %+v", result)
	}
}

func TestGetCondition_ReturnsCorrectCondition(t *testing.T) {
	conditions := []metav1.Condition{
		{Type: TypeGitRepoReady, Status: metav1.ConditionTrue, Reason: ReasonReady},
		{Type: TypeBuildNamespaceReady, Status: metav1.ConditionFalse, Reason: ReasonReconciling},
		{Type: TypeQuotaEnforced, Status: metav1.ConditionTrue, Reason: ReasonReady},
	}

	result := GetCondition(conditions, TypeBuildNamespaceReady)
	if result == nil {
		t.Fatal("expected non-nil condition, got nil")
		return // unreachable, but satisfies staticcheck SA5011
	}
	if result.Type != TypeBuildNamespaceReady {
		t.Errorf("expected type %q, got %q", TypeBuildNamespaceReady, result.Type)
	}
	if result.Status != metav1.ConditionFalse {
		t.Errorf("expected status False, got %s", result.Status)
	}
}

func TestIsConditionTrue_ReturnsFalseForMissingCondition(t *testing.T) {
	var conditions []metav1.Condition

	if IsConditionTrue(conditions, TypeGitRepoReady) {
		t.Error("expected false for missing condition, got true")
	}
}

func TestIsConditionTrue_ReturnsFalseForFalseCondition(t *testing.T) {
	conditions := []metav1.Condition{
		{Type: TypeGitRepoReady, Status: metav1.ConditionFalse},
	}

	if IsConditionTrue(conditions, TypeGitRepoReady) {
		t.Error("expected false for False condition, got true")
	}
}

func TestIsConditionTrue_ReturnsTrueForTrueCondition(t *testing.T) {
	conditions := []metav1.Condition{
		{Type: TypeGitRepoReady, Status: metav1.ConditionTrue},
	}

	if !IsConditionTrue(conditions, TypeGitRepoReady) {
		t.Error("expected true for True condition, got false")
	}
}

func TestIsConditionTrueForGeneration_ReturnsFalseWhenGenerationMismatch(t *testing.T) {
	conditions := []metav1.Condition{
		{
			Type:               TypeGitRepoReady,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: 5,
		},
	}

	if IsConditionTrueForGeneration(conditions, TypeGitRepoReady, 6) {
		t.Error("expected false when generation mismatches, got true")
	}
}

func TestIsConditionTrueForGeneration_ReturnsTrueWhenGenerationMatches(t *testing.T) {
	conditions := []metav1.Condition{
		{
			Type:               TypeGitRepoReady,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: 5,
		},
	}

	if !IsConditionTrueForGeneration(conditions, TypeGitRepoReady, 5) {
		t.Error("expected true when condition is True and generation matches, got false")
	}
}

func TestIsConditionTrueForGeneration_ReturnsFalseWhenConditionFalse(t *testing.T) {
	conditions := []metav1.Condition{
		{
			Type:               TypeGitRepoReady,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: 5,
		},
	}

	if IsConditionTrueForGeneration(conditions, TypeGitRepoReady, 5) {
		t.Error("expected false when condition is False even with matching generation, got true")
	}
}

func TestAllConditionsTrue_ReturnsTrueWhenAllPresentAndTrue(t *testing.T) {
	conditions := []metav1.Condition{
		{Type: TypeGitRepoReady, Status: metav1.ConditionTrue},
		{Type: TypeBuildNamespaceReady, Status: metav1.ConditionTrue},
		{Type: TypeQuotaEnforced, Status: metav1.ConditionTrue},
	}

	if !AllConditionsTrue(conditions, TypeGitRepoReady, TypeBuildNamespaceReady, TypeQuotaEnforced) {
		t.Error("expected true when all conditions are True, got false")
	}
}

func TestAllConditionsTrue_ReturnsFalseWhenOneMissing(t *testing.T) {
	conditions := []metav1.Condition{
		{Type: TypeGitRepoReady, Status: metav1.ConditionTrue},
		{Type: TypeBuildNamespaceReady, Status: metav1.ConditionTrue},
	}

	if AllConditionsTrue(conditions, TypeGitRepoReady, TypeBuildNamespaceReady, TypeQuotaEnforced) {
		t.Error("expected false when one condition is missing, got true")
	}
}

func TestAllConditionsTrue_ReturnsFalseWhenOneFalse(t *testing.T) {
	conditions := []metav1.Condition{
		{Type: TypeGitRepoReady, Status: metav1.ConditionTrue},
		{Type: TypeBuildNamespaceReady, Status: metav1.ConditionFalse},
		{Type: TypeQuotaEnforced, Status: metav1.ConditionTrue},
	}

	if AllConditionsTrue(conditions, TypeGitRepoReady, TypeBuildNamespaceReady, TypeQuotaEnforced) {
		t.Error("expected false when one condition is False, got true")
	}
}
