// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package conditions

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// makeDetail constructs a detail condition for readiness summary tests.
func makeDetail(t string, s metav1.ConditionStatus, reason string, gen int64) metav1.Condition {
	return metav1.Condition{
		Type:               t,
		Status:             s,
		Reason:             reason,
		ObservedGeneration: gen,
	}
}

// summaryStatus returns the (Ready, Reconciling, Stalled) status triple.
func summaryStatus(conds []metav1.Condition) (metav1.ConditionStatus, metav1.ConditionStatus, metav1.ConditionStatus) {
	r := GetCondition(conds, TypeReady)
	rc := GetCondition(conds, TypeReconciling)
	st := GetCondition(conds, TypeStalled)
	val := func(c *metav1.Condition) metav1.ConditionStatus {
		if c == nil {
			return "Missing"
		}
		return c.Status
	}
	return val(r), val(rc), val(st)
}

// TestSetReadinessSummaryViaPkg_IdempotentNoop — k8s-controller-engineer
// condition #2 (mandatory FIRST test): calling twice with same input
// produces zero LastTransitionTime change. Otherwise every reconcile
// becomes a status write, defeating the predicates from T2.1.
func TestSetReadinessSummaryViaPkg_IdempotentNoop(t *testing.T) {
	gen := int64(5)
	conds := []metav1.Condition{
		makeDetail("A", metav1.ConditionTrue, ReasonReady, gen),
		makeDetail("B", metav1.ConditionTrue, ReasonReady, gen),
	}
	SetReadinessSummaryViaPkg(&conds, gen, []string{"A", "B"})
	first := GetCondition(conds, TypeReady).LastTransitionTime
	if first.IsZero() {
		t.Fatalf("first call did not set LastTransitionTime")
	}
	SetReadinessSummaryViaPkg(&conds, gen, []string{"A", "B"})
	second := GetCondition(conds, TypeReady).LastTransitionTime
	if !first.Equal(&second) {
		t.Errorf("LastTransitionTime changed on idempotent re-call: %v → %v", first, second)
	}
}

// TestSetReadinessSummaryViaPkg_AllTrue — happy path: all details True with
// matching generation → Ready=True.
func TestSetReadinessSummaryViaPkg_AllTrue(t *testing.T) {
	gen := int64(1)
	conds := []metav1.Condition{
		makeDetail("A", metav1.ConditionTrue, ReasonReady, gen),
		makeDetail("B", metav1.ConditionTrue, ReasonReady, gen),
	}
	SetReadinessSummaryViaPkg(&conds, gen, []string{"A", "B"})
	r, rc, st := summaryStatus(conds)
	if r != metav1.ConditionTrue || rc != metav1.ConditionFalse || st != metav1.ConditionFalse {
		t.Errorf("AllTrue: got Ready=%s Reconciling=%s Stalled=%s; want True/False/False", r, rc, st)
	}
}

// TestSetReadinessSummaryViaPkg_OneReconciling — one detail False with
// reason Reconciling → summary Reconciling=True (Stalled stays False).
func TestSetReadinessSummaryViaPkg_OneReconciling(t *testing.T) {
	gen := int64(1)
	conds := []metav1.Condition{
		makeDetail("A", metav1.ConditionTrue, ReasonReady, gen),
		makeDetail("B", metav1.ConditionFalse, ReasonReconciling, gen),
	}
	SetReadinessSummaryViaPkg(&conds, gen, []string{"A", "B"})
	r, rc, st := summaryStatus(conds)
	if r != metav1.ConditionFalse || rc != metav1.ConditionTrue || st != metav1.ConditionFalse {
		t.Errorf("OneReconciling: got Ready=%s Reconciling=%s Stalled=%s; want False/True/False", r, rc, st)
	}
}

// TestSetReadinessSummaryViaPkg_OneFailed — one detail False with reason
// Failed → Stalled=True (wins over Reconciling).
func TestSetReadinessSummaryViaPkg_OneFailed(t *testing.T) {
	gen := int64(1)
	conds := []metav1.Condition{
		makeDetail("A", metav1.ConditionTrue, ReasonReady, gen),
		makeDetail("B", metav1.ConditionFalse, ReasonFailed, gen),
	}
	SetReadinessSummaryViaPkg(&conds, gen, []string{"A", "B"})
	r, rc, st := summaryStatus(conds)
	if r != metav1.ConditionFalse || rc != metav1.ConditionFalse || st != metav1.ConditionTrue {
		t.Errorf("OneFailed: got Ready=%s Reconciling=%s Stalled=%s; want False/False/True", r, rc, st)
	}
}

// TestSetReadinessSummaryViaPkg_FailedWinsOverReconciling — failure takes
// priority. Both Failed (B) and Reconciling (C) → Stalled=True.
func TestSetReadinessSummaryViaPkg_FailedWinsOverReconciling(t *testing.T) {
	gen := int64(1)
	conds := []metav1.Condition{
		makeDetail("A", metav1.ConditionTrue, ReasonReady, gen),
		makeDetail("B", metav1.ConditionFalse, ReasonFailed, gen),
		makeDetail("C", metav1.ConditionFalse, ReasonReconciling, gen),
	}
	SetReadinessSummaryViaPkg(&conds, gen, []string{"A", "B", "C"})
	_, _, st := summaryStatus(conds)
	if st != metav1.ConditionTrue {
		t.Errorf("FailedWinsOverReconciling: Stalled=%s; want True", st)
	}
}

// TestSetReadinessSummaryViaPkg_EmptyDetail — degenerate case: no detail
// types → Ready=True, no work for controller.
func TestSetReadinessSummaryViaPkg_EmptyDetail(t *testing.T) {
	gen := int64(1)
	conds := []metav1.Condition{}
	SetReadinessSummaryViaPkg(&conds, gen, nil)
	r, rc, st := summaryStatus(conds)
	if r != metav1.ConditionTrue || rc != metav1.ConditionFalse || st != metav1.ConditionFalse {
		t.Errorf("EmptyDetail: got Ready=%s Reconciling=%s Stalled=%s; want True/False/False", r, rc, st)
	}
}

// TestSetReadinessSummaryViaPkg_GenerationMismatch — detail True but
// observedGeneration < current → not Ready (controller hasn't caught
// up to spec change).
func TestSetReadinessSummaryViaPkg_GenerationMismatch(t *testing.T) {
	conds := []metav1.Condition{
		makeDetail("A", metav1.ConditionTrue, ReasonReady, 1),
		makeDetail("B", metav1.ConditionTrue, ReasonReady, 1),
	}
	SetReadinessSummaryViaPkg(&conds, 2, []string{"A", "B"})
	r, rc, st := summaryStatus(conds)
	if r != metav1.ConditionFalse || rc != metav1.ConditionTrue || st != metav1.ConditionFalse {
		t.Errorf("GenerationMismatch: got Ready=%s Reconciling=%s Stalled=%s; want False/True/False", r, rc, st)
	}
}

// TestSetReadinessSummaryViaPkg_PartialGenerationMismatch — k8s-controller-
// engineer condition #2: 3 of 5 details current, 2 stale → Reconciling.
func TestSetReadinessSummaryViaPkg_PartialGenerationMismatch(t *testing.T) {
	conds := []metav1.Condition{
		makeDetail("A", metav1.ConditionTrue, ReasonReady, 2),
		makeDetail("B", metav1.ConditionTrue, ReasonReady, 2),
		makeDetail("C", metav1.ConditionTrue, ReasonReady, 2),
		makeDetail("D", metav1.ConditionTrue, ReasonReady, 1), // stale
		makeDetail("E", metav1.ConditionTrue, ReasonReady, 1), // stale
	}
	SetReadinessSummaryViaPkg(&conds, 2, []string{"A", "B", "C", "D", "E"})
	r, _, _ := summaryStatus(conds)
	if r != metav1.ConditionFalse {
		t.Errorf("PartialGenerationMismatch: Ready=%s; want False (some stale)", r)
	}
}

// TestSetReadinessSummaryViaPkg_MissingDetailIsReconciling — detail not yet
// emitted → treated as in-progress. kstatus.Compute returns InProgress.
func TestSetReadinessSummaryViaPkg_MissingDetailIsReconciling(t *testing.T) {
	gen := int64(1)
	conds := []metav1.Condition{
		makeDetail("A", metav1.ConditionTrue, ReasonReady, gen),
		// "B" missing — controller hasn't set it yet
	}
	SetReadinessSummaryViaPkg(&conds, gen, []string{"A", "B"})
	r, rc, st := summaryStatus(conds)
	if r != metav1.ConditionFalse || rc != metav1.ConditionTrue || st != metav1.ConditionFalse {
		t.Errorf("MissingDetail: got Ready=%s Reconciling=%s Stalled=%s; want False/True/False", r, rc, st)
	}
}

// TestSetReadinessSummaryViaPkg_UnknownStatusIsReconciling — kstatus
// convention: Unknown classified as in-progress.
func TestSetReadinessSummaryViaPkg_UnknownStatusIsReconciling(t *testing.T) {
	gen := int64(1)
	conds := []metav1.Condition{
		makeDetail("A", metav1.ConditionTrue, ReasonReady, gen),
		makeDetail("B", metav1.ConditionUnknown, ReasonNotFound, gen),
	}
	SetReadinessSummaryViaPkg(&conds, gen, []string{"A", "B"})
	r, rc, _ := summaryStatus(conds)
	if r != metav1.ConditionFalse || rc != metav1.ConditionTrue {
		t.Errorf("UnknownIsReconciling: got Ready=%s Reconciling=%s; want False/True", r, rc)
	}
}

// TestSetReadinessSummaryViaPkg_DegradedIsStalled — Degraded reason
// classified as failure (kstatus Stalled; ReasonDegraded constant).
func TestSetReadinessSummaryViaPkg_DegradedIsStalled(t *testing.T) {
	gen := int64(1)
	conds := []metav1.Condition{
		makeDetail("A", metav1.ConditionFalse, ReasonDegraded, gen),
	}
	SetReadinessSummaryViaPkg(&conds, gen, []string{"A"})
	_, _, st := summaryStatus(conds)
	if st != metav1.ConditionTrue {
		t.Errorf("DegradedIsStalled: Stalled=%s; want True", st)
	}
}

// TestSetReadinessSummaryViaPkg_StalledTrueToFalse_PreservesCondition —
// recovery path. When all details flip back to True after a Stalled,
// the Stalled condition MUST be set to False (not removed) to preserve
// audit trail. K8s convention: keep the condition with status flipped.
func TestSetReadinessSummaryViaPkg_StalledTrueToFalse_PreservesCondition(t *testing.T) {
	gen := int64(1)
	conds := []metav1.Condition{
		makeDetail("A", metav1.ConditionFalse, ReasonFailed, gen),
	}
	SetReadinessSummaryViaPkg(&conds, gen, []string{"A"})
	_, _, st := summaryStatus(conds)
	if st != metav1.ConditionTrue {
		t.Fatalf("setup precondition: Stalled=%s; want True", st)
	}

	// Recovery: detail flips back to True.
	SetCondition(&conds, "A", metav1.ConditionTrue, ReasonReady, "recovered", gen)
	SetReadinessSummaryViaPkg(&conds, gen, []string{"A"})

	stalledCond := GetCondition(conds, TypeStalled)
	if stalledCond == nil {
		t.Fatalf("Stalled condition removed; expected preserved with status=False")
	}
	if stalledCond.Status != metav1.ConditionFalse {
		t.Errorf("Stalled condition status=%s; want False", stalledCond.Status)
	}
}

// TestSetReadinessSummaryViaPkg_NilSlice_NoPanic — defensive check: nil
// pointer must not panic. Logging-friendly fail-soft.
func TestSetReadinessSummaryViaPkg_NilSlice_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SetReadinessSummaryViaPkg panicked on nil slice: %v", r)
		}
	}()
	SetReadinessSummaryViaPkg(nil, 1, []string{"A"})
}

// TestSetReadinessSummaryViaPkg_DetailWithZeroGeneration_NotStale — detail
// emitted before any spec edit (gen=0) MUST NOT be flagged as stale,
// otherwise initial reconciles never reach Ready. Controller emits
// observedGeneration=0 for the very first reconcile pass.
func TestSetReadinessSummaryViaPkg_DetailWithZeroGeneration_NotStale(t *testing.T) {
	conds := []metav1.Condition{
		makeDetail("A", metav1.ConditionTrue, ReasonReady, 0),
	}
	SetReadinessSummaryViaPkg(&conds, 1, []string{"A"})
	r, _, _ := summaryStatus(conds)
	if r != metav1.ConditionTrue {
		t.Errorf("ZeroGeneration: Ready=%s; want True (gen=0 means controller hasn't observed yet, treat as not-stale)", r)
	}
}
