// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package conditions

import (
	"testing"
	"time"

	pkgconditions "github.com/getloopscale/loop-runtime/pkg/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
