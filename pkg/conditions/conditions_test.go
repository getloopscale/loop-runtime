// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package conditions

import (
	"testing"
	"time"
)

// fixedClock returns a deterministic time for tests.
func fixedClock(year int) func() time.Time {
	t := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
	return func() time.Time { return t }
}

// withFixedClock installs a fixed clock and returns a cleanup func suitable
// for t.Cleanup(...).
func withFixedClock(year int) func() {
	prev := SetClock(fixedClock(year))
	return func() { SetClock(prev) }
}

func TestSetCondition_AppendsWhenAbsent(t *testing.T) {
	t.Cleanup(withFixedClock(2026))

	var conds []Condition
	changed := SetCondition(&conds, TypeReady, StatusTrue, ReasonReady, "ok", 1)
	if !changed {
		t.Error("appending a new condition must report changed=true")
	}
	if len(conds) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conds))
	}
	if conds[0].Type != TypeReady || conds[0].Status != StatusTrue {
		t.Errorf("unexpected condition: %+v", conds[0])
	}
}

func TestSetCondition_PreservesTransitionTimeWhenStatusUnchanged(t *testing.T) {
	t.Cleanup(withFixedClock(2026))

	var conds []Condition
	SetCondition(&conds, TypeReady, StatusTrue, ReasonReady, "ok", 1)
	original := conds[0].LastTransitionTime

	// Advance time and call again with same Status.
	SetClock(func() time.Time {
		return time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	})
	changed := SetCondition(&conds, TypeReady, StatusTrue, ReasonReady, "still ok", 2)
	if changed {
		t.Error("metadata-only update must report changed=false")
	}
	if !conds[0].LastTransitionTime.Equal(original) {
		t.Errorf("LastTransitionTime should be preserved when Status unchanged; was %v now %v",
			original, conds[0].LastTransitionTime)
	}
	if conds[0].Message != "still ok" || conds[0].ObservedGeneration != 2 {
		t.Errorf("metadata fields must be updated: %+v", conds[0])
	}
}

func TestSetCondition_UpdatesTransitionTimeOnStatusChange(t *testing.T) {
	t.Cleanup(withFixedClock(2026))

	var conds []Condition
	SetCondition(&conds, TypeReady, StatusTrue, ReasonReady, "ok", 1)
	original := conds[0].LastTransitionTime

	later := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	SetClock(func() time.Time { return later })
	changed := SetCondition(&conds, TypeReady, StatusFalse, ReasonReconciling, "starting over", 1)
	if !changed {
		t.Error("status flip must report changed=true")
	}
	if conds[0].LastTransitionTime.Equal(original) {
		t.Error("LastTransitionTime should advance on status flip")
	}
	if !conds[0].LastTransitionTime.Equal(later) {
		t.Errorf("LastTransitionTime should be the new clock value; got %v", conds[0].LastTransitionTime)
	}
}

func TestSetCondition_NilSliceReturnsFalse(t *testing.T) {
	if SetCondition(nil, TypeReady, StatusTrue, ReasonReady, "", 1) {
		t.Error("SetCondition(nil) must return false; not panic")
	}
}

func TestGetCondition_ReturnsPointerOrNil(t *testing.T) {
	conds := []Condition{
		{Type: TypeReady, Status: StatusTrue, Reason: ReasonReady},
	}
	if got := GetCondition(conds, TypeReady); got == nil || got.Status != StatusTrue {
		t.Errorf("expected Ready=True, got %+v", got)
	}
	if got := GetCondition(conds, TypeStalled); got != nil {
		t.Errorf("expected nil for absent type, got %+v", got)
	}
}

func TestIsConditionTrue(t *testing.T) {
	conds := []Condition{
		{Type: TypeReady, Status: StatusTrue},
		{Type: TypeStalled, Status: StatusFalse},
	}
	if !IsConditionTrue(conds, TypeReady) {
		t.Error("IsConditionTrue(Ready) should be true")
	}
	if IsConditionTrue(conds, TypeStalled) {
		t.Error("IsConditionTrue(Stalled=False) should be false")
	}
	if IsConditionTrue(conds, "Absent") {
		t.Error("IsConditionTrue for absent type should be false")
	}
}

func TestIsConditionTrueForGeneration(t *testing.T) {
	conds := []Condition{
		{Type: TypeReady, Status: StatusTrue, ObservedGeneration: 3},
	}
	if !IsConditionTrueForGeneration(conds, TypeReady, 3) {
		t.Error("matching generation should return true")
	}
	if IsConditionTrueForGeneration(conds, TypeReady, 4) {
		t.Error("mismatched generation should return false")
	}
}

func TestAllConditionsTrue(t *testing.T) {
	conds := []Condition{
		{Type: "A", Status: StatusTrue},
		{Type: "B", Status: StatusTrue},
		{Type: "C", Status: StatusFalse},
	}
	if !AllConditionsTrue(conds, "A", "B") {
		t.Error("expected true when all listed are True")
	}
	if AllConditionsTrue(conds, "A", "C") {
		t.Error("expected false when one listed is False")
	}
	if AllConditionsTrue(conds, "MISSING") {
		t.Error("expected false when listed type is absent")
	}
}

// --- Standard type/reason regression guards ---

func TestStandardTypeConstants(t *testing.T) {
	cases := []struct{ got, want, name string }{
		{TypeReady, "Ready", "TypeReady"},
		{TypeReconciling, "Reconciling", "TypeReconciling"},
		{TypeStalled, "Stalled", "TypeStalled"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestStandardReasonConstants(t *testing.T) {
	// Each Reason* constant must have a stable string representation.
	want := map[string]string{
		"ReasonReady":       "Ready",
		"ReasonReconciling": "Reconciling",
		"ReasonProgressing": "Progressing",
		"ReasonWaiting":     "Waiting",
		"ReasonNotFound":    "NotFound",
		"ReasonUnreachable": "Unreachable",
		"ReasonFailed":      "Failed",
		"ReasonStalled":     "Stalled",
		"ReasonDegraded":    "Degraded",
	}
	got := map[string]string{
		"ReasonReady":       ReasonReady,
		"ReasonReconciling": ReasonReconciling,
		"ReasonProgressing": ReasonProgressing,
		"ReasonWaiting":     ReasonWaiting,
		"ReasonNotFound":    ReasonNotFound,
		"ReasonUnreachable": ReasonUnreachable,
		"ReasonFailed":      ReasonFailed,
		"ReasonStalled":     ReasonStalled,
		"ReasonDegraded":    ReasonDegraded,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}
