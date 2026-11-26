// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package conditions

import (
	"strings"
	"testing"
)

// detail is a small helper for building Conditions in test fixtures.
func detail(t string, s Status, reason string, gen int64) Condition {
	return Condition{Type: t, Status: s, Reason: reason, ObservedGeneration: gen}
}

func TestSummary_EmptyDetails_MarksReady(t *testing.T) {
	t.Cleanup(withFixedClock(2026))
	var conds []Condition
	SetReadinessSummary(&conds, 1, nil)

	if !IsConditionTrue(conds, TypeReady) {
		t.Error("empty detailTypes should produce Ready=True")
	}
	if IsConditionTrue(conds, TypeReconciling) {
		t.Error("Reconciling must be False when there are no details")
	}
	if IsConditionTrue(conds, TypeStalled) {
		t.Error("Stalled must be False when there are no details")
	}
}

func TestSummary_AllTrueWithMatchingGeneration_MarksReady(t *testing.T) {
	t.Cleanup(withFixedClock(2026))
	conds := []Condition{
		detail("Step1", StatusTrue, ReasonReady, 1),
		detail("Step2", StatusTrue, ReasonReady, 1),
	}
	SetReadinessSummary(&conds, 1, []string{"Step1", "Step2"})

	if !IsConditionTrue(conds, TypeReady) {
		t.Error("all-True with matching generation must produce Ready=True")
	}
	if IsConditionTrue(conds, TypeReconciling) {
		t.Error("Reconciling must be False when ready")
	}
	if IsConditionTrue(conds, TypeStalled) {
		t.Error("Stalled must be False when ready")
	}
}

func TestSummary_FailedReason_MarksStalled(t *testing.T) {
	t.Cleanup(withFixedClock(2026))
	conds := []Condition{
		detail("Step1", StatusTrue, ReasonReady, 1),
		detail("Step2", StatusFalse, ReasonFailed, 1),
	}
	SetReadinessSummary(&conds, 1, []string{"Step1", "Step2"})

	if !IsConditionTrue(conds, TypeStalled) {
		t.Error("Failed reason must cascade to Stalled=True")
	}
	stalled := GetCondition(conds, TypeStalled)
	if !strings.Contains(stalled.Message, "Step2") {
		t.Errorf("Stalled message should name the failed detail: %s", stalled.Message)
	}
}

func TestSummary_DegradedReason_MarksStalled(t *testing.T) {
	t.Cleanup(withFixedClock(2026))
	conds := []Condition{
		detail("Step1", StatusFalse, ReasonDegraded, 1),
	}
	SetReadinessSummary(&conds, 1, []string{"Step1"})
	if !IsConditionTrue(conds, TypeStalled) {
		t.Error("Degraded reason must cascade to Stalled=True")
	}
}

func TestSummary_StalledReason_MarksStalled(t *testing.T) {
	t.Cleanup(withFixedClock(2026))
	conds := []Condition{
		detail("Step1", StatusFalse, ReasonStalled, 1),
	}
	SetReadinessSummary(&conds, 1, []string{"Step1"})
	if !IsConditionTrue(conds, TypeStalled) {
		t.Error("Stalled reason must cascade to Stalled=True")
	}
}

func TestSummary_StalledWinsOverReconciling(t *testing.T) {
	t.Cleanup(withFixedClock(2026))
	conds := []Condition{
		detail("Step1", StatusFalse, ReasonReconciling, 1), // would be Reconciling alone
		detail("Step2", StatusFalse, ReasonFailed, 1),      // promotes to Stalled
	}
	SetReadinessSummary(&conds, 1, []string{"Step1", "Step2"})
	if !IsConditionTrue(conds, TypeStalled) {
		t.Error("Failed reason MUST take precedence over Reconciling reason")
	}
	if IsConditionTrue(conds, TypeReconciling) {
		t.Error("Reconciling must be False when Stalled wins")
	}
}

func TestSummary_MissingDetail_MarksReconciling(t *testing.T) {
	t.Cleanup(withFixedClock(2026))
	var conds []Condition
	SetReadinessSummary(&conds, 1, []string{"Step1"})
	if !IsConditionTrue(conds, TypeReconciling) {
		t.Error("missing detail must cascade to Reconciling=True")
	}
	rec := GetCondition(conds, TypeReconciling)
	if !strings.Contains(rec.Message, "awaiting") {
		t.Errorf("missing-detail message should say 'awaiting': %s", rec.Message)
	}
}

func TestSummary_GenerationMismatchOnTrue_MarksReconciling(t *testing.T) {
	t.Cleanup(withFixedClock(2026))
	conds := []Condition{
		detail("Step1", StatusTrue, ReasonReady, 1), // observed gen 1, current is 2
	}
	SetReadinessSummary(&conds, 2, []string{"Step1"})
	if IsConditionTrue(conds, TypeReady) {
		t.Error("generation mismatch must NOT mark Ready")
	}
	if !IsConditionTrue(conds, TypeReconciling) {
		t.Error("generation mismatch should be reported as Reconciling")
	}
	rec := GetCondition(conds, TypeReconciling)
	if !strings.Contains(rec.Message, "spec changed") {
		t.Errorf("expected generation-mismatch message: %s", rec.Message)
	}
}

func TestSummary_UnknownStatus_MarksReconciling(t *testing.T) {
	t.Cleanup(withFixedClock(2026))
	conds := []Condition{
		detail("Step1", StatusUnknown, "", 1),
	}
	SetReadinessSummary(&conds, 1, []string{"Step1"})
	if !IsConditionTrue(conds, TypeReconciling) {
		t.Error("Unknown status should cascade to Reconciling")
	}
}

func TestSummary_Idempotent(t *testing.T) {
	t.Cleanup(withFixedClock(2026))
	conds := []Condition{
		detail("Step1", StatusTrue, ReasonReady, 1),
	}
	SetReadinessSummary(&conds, 1, []string{"Step1"})
	first := GetCondition(conds, TypeReady).LastTransitionTime
	SetReadinessSummary(&conds, 1, []string{"Step1"})
	second := GetCondition(conds, TypeReady).LastTransitionTime
	if !first.Equal(second) {
		t.Errorf("idempotent call must not advance LastTransitionTime; first=%v second=%v",
			first, second)
	}
}

func TestSummary_NilSliceIsNoOp(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SetReadinessSummary(nil) must not panic: %v", r)
		}
	}()
	SetReadinessSummary(nil, 1, []string{"Step1"})
}
