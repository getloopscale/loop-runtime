// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestIsPodReady (M25): covers the happy path plus the two negative cases
// (not-Running, Running-but-no-Ready-condition) — the untested-behavior the
// review flagged. testing.md § 4.1: negative + edge cases, not just happy.
func TestIsPodReady(t *testing.T) {
	ready := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		},
	}
	pending := &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodPending}}
	runningNotReady := &corev1.Pod{
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse}},
		},
	}
	cases := []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{"running+ready", ready, true},
		{"pending", pending, false},
		{"running-but-not-ready", runningNotReady, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isPodReady(c.pod); got != c.want {
				t.Errorf("isPodReady(%s) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

// TestFirstReadyPod (M25): happy path (a Ready pod is found by name) + negative
// path (no Ready pod → typed error), against a fake client.
func TestFirstReadyPod(t *testing.T) {
	labels := map[string]string{"app.kubernetes.io/name": "svc"}
	mkPod := func(name string, ready bool) *corev1.Pod {
		p := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", Labels: labels},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		}
		st := corev1.ConditionFalse
		if ready {
			st = corev1.ConditionTrue
		}
		p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: st}}
		return p
	}

	t.Run("finds ready pod", func(t *testing.T) {
		c := fake.NewClientBuilder().WithObjects(mkPod("not-ready", false), mkPod("ready-one", true)).Build()
		got, err := firstReadyPod(context.Background(), c, "ns", labels)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "ready-one" {
			t.Errorf("got %q, want ready-one", got)
		}
	})

	t.Run("no ready pod -> error", func(t *testing.T) {
		c := fake.NewClientBuilder().WithObjects(mkPod("p1", false)).Build()
		if _, err := firstReadyPod(context.Background(), c, "ns", labels); err == nil {
			t.Fatal("want error when no Ready pod, got nil")
		}
	})
}
