// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package conditions

import (
	"testing"
	"time"

	pkgconditions "github.com/getloopscale/loopzero/pkg/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestSetReadinessSummaryViaPkg_SemanticsMatchOperatorLocal verifies
// that delegating to pkg/conditions produces output equivalent to the
// operator-local implementation (the algorithmic core is shared, so
// any drift would be a regression).
func TestSetReadinessSummaryViaPkg_SemanticsMatchOperatorLocal(t *testing.T) {
	cases := []struct {
		name        string
		seed        []metav1.Condition
		generation  int64
		detailTypes []string
	}{
		{
			name: "all True — Ready",
			seed: []metav1.Condition{
				{Type: "Step1", Status: metav1.ConditionTrue, Reason: ReasonReady, ObservedGeneration: 1},
				{Type: "Step2", Status: metav1.ConditionTrue, Reason: ReasonReady, ObservedGeneration: 1},
			},
			generation:  1,
			detailTypes: []string{"Step1", "Step2"},
		},
		{
			name: "Failed — Stalled",
			seed: []metav1.Condition{
				{Type: "Step1", Status: metav1.ConditionTrue, Reason: ReasonReady, ObservedGeneration: 1},
				{Type: "Step2", Status: metav1.ConditionFalse, Reason: ReasonFailed, ObservedGeneration: 1},
			},
			generation:  1,
			detailTypes: []string{"Step1", "Step2"},
		},
		{
			name:        "missing detail — Reconciling",
			seed:        []metav1.Condition{},
			generation:  1,
			detailTypes: []string{"Step1"},
		},
		{
			name: "generation mismatch — Reconciling",
			seed: []metav1.Condition{
				{Type: "Step1", Status: metav1.ConditionTrue, Reason: ReasonReady, ObservedGeneration: 1},
			},
			generation:  2,
			detailTypes: []string{"Step1"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Run via local + via pkg in lockstep with identical seeds.
			localCopy := append([]metav1.Condition(nil), c.seed...)
			pkgCopy := append([]metav1.Condition(nil), c.seed...)

			SetReadinessSummary(&localCopy, c.generation, c.detailTypes)
			SetReadinessSummaryViaPkg(&pkgCopy, c.generation, c.detailTypes)

			summary := func(s []metav1.Condition, typ string) *metav1.Condition {
				for i := range s {
					if s[i].Type == typ {
						return &s[i]
					}
				}
				return nil
			}

			for _, t3 := range []string{TypeReady, TypeReconciling, TypeStalled} {
				l := summary(localCopy, t3)
				p := summary(pkgCopy, t3)
				if (l == nil) != (p == nil) {
					t.Errorf("%s presence diverges: local=%v pkg=%v", t3, l != nil, p != nil)
					continue
				}
				if l == nil {
					continue
				}
				// Status must match. Reason+message match in shared algo;
				// LastTransitionTime can differ across calls so we
				// don't compare it.
				if l.Status != p.Status {
					t.Errorf("%s.Status mismatch: local=%s pkg=%s", t3, l.Status, p.Status)
				}
				if l.Reason != p.Reason {
					t.Errorf("%s.Reason mismatch: local=%q pkg=%q", t3, l.Reason, p.Reason)
				}
			}
		})
	}
}

func TestFromMetav1_RoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	original := []metav1.Condition{
		{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			Message:            "all good",
			LastTransitionTime: metav1.NewTime(now),
			ObservedGeneration: 7,
		},
	}
	portable := fromMetav1(original)
	roundtripped := toMetav1(portable)

	if len(roundtripped) != 1 {
		t.Fatalf("round-trip length lost: got %d", len(roundtripped))
	}
	r := roundtripped[0]
	if r.Type != "Ready" || r.Status != metav1.ConditionTrue {
		t.Errorf("type/status drifted: %+v", r)
	}
	if r.Reason != "Ready" || r.Message != "all good" {
		t.Errorf("reason/message drifted: %+v", r)
	}
	if !r.LastTransitionTime.Time.Equal(now) {
		t.Errorf("LastTransitionTime drifted: got %v want %v", r.LastTransitionTime, now)
	}
	if r.ObservedGeneration != 7 {
		t.Errorf("ObservedGeneration drifted: got %d", r.ObservedGeneration)
	}
}

func TestFromMetav1_StatusEnumPreservation(t *testing.T) {
	cases := []metav1.ConditionStatus{
		metav1.ConditionTrue,
		metav1.ConditionFalse,
		metav1.ConditionUnknown,
	}
	for _, s := range cases {
		c := []metav1.Condition{{Type: "X", Status: s}}
		p := fromMetav1(c)
		back := toMetav1(p)
		if back[0].Status != s {
			t.Errorf("Status %q lost in round-trip; got %q", s, back[0].Status)
		}
	}
}

func TestSetReadinessSummaryViaPkg_NilSliceIsNoOp(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil slice must not panic: %v", r)
		}
	}()
	SetReadinessSummaryViaPkg(nil, 1, []string{"X"})
}

// Compile-time verification: pkg/conditions Status enum strings are
// identical to metav1.ConditionStatus. Drift would silently corrupt
// the round-trip.
func TestPkgStatusValuesMatchMetav1(t *testing.T) {
	if string(pkgconditions.StatusTrue) != string(metav1.ConditionTrue) {
		t.Errorf("StatusTrue drift: %q vs %q", pkgconditions.StatusTrue, metav1.ConditionTrue)
	}
	if string(pkgconditions.StatusFalse) != string(metav1.ConditionFalse) {
		t.Errorf("StatusFalse drift: %q vs %q", pkgconditions.StatusFalse, metav1.ConditionFalse)
	}
	if string(pkgconditions.StatusUnknown) != string(metav1.ConditionUnknown) {
		t.Errorf("StatusUnknown drift: %q vs %q", pkgconditions.StatusUnknown, metav1.ConditionUnknown)
	}
}
