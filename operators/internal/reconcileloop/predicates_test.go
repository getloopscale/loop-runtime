// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package reconcileloop

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// Test_OperatorPredicates_StatusOnlyUpdate_Filtered — guard test for the
// 50–500x reconcile amplification fix per ADR-0023 / plan T2.1. A pure
// status update (no spec/labels/annotations change) MUST be filtered.
//
// The Or composition uses Generation/Annotation/Label predicates; none
// fire on metav1.Condition slice mutation alone (Status is its own
// subresource and doesn't bump generation/annotations/labels).
func Test_OperatorPredicates_StatusOnlyUpdate_Filtered(t *testing.T) {
	p := OperatorPredicates()

	// Two ConfigMap snapshots with identical Spec/metadata; only Annotations
	// (representing status-equivalent state in this test fixture) differ:
	// no — actually we want IDENTICAL annotations and labels and generation.
	old := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "cm",
			Namespace:   "default",
			Generation:  1,
			Annotations: map[string]string{"a": "b"},
			Labels:      map[string]string{"l": "v"},
		},
	}
	updated := old.DeepCopy()
	// Simulate a status update: no metadata change. Resource version may
	// differ but predicates don't check RV.
	updated.ResourceVersion = "2"

	got := p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: updated})
	if got {
		t.Errorf("OperatorPredicates.Update returned true on status-only change; expected false (filtered)")
	}
}

// Test_OperatorPredicates_GenerationChange_Reconciled — spec mutation
// MUST trigger reconcile (generation bumps).
func Test_OperatorPredicates_GenerationChange_Reconciled(t *testing.T) {
	p := OperatorPredicates()

	old := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Generation: 1},
	}
	updated := old.DeepCopy()
	updated.Generation = 2

	if !p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: updated}) {
		t.Errorf("OperatorPredicates.Update returned false on generation bump; expected true")
	}
}

// Test_OperatorPredicates_AnnotationChange_Reconciled — annotation
// mutation triggers reconcile (e.g., runtime.getloopscale.com/heal-trigger,
// runtime.getloopscale.com/cell-id pin).
func Test_OperatorPredicates_AnnotationChange_Reconciled(t *testing.T) {
	p := OperatorPredicates()

	old := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Generation:  1,
			Annotations: map[string]string{"a": "b"},
		},
	}
	updated := old.DeepCopy()
	updated.Annotations["runtime.getloopscale.com/heal-trigger"] = "1234567890"

	if !p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: updated}) {
		t.Errorf("OperatorPredicates.Update returned false on annotation add; expected true")
	}
}

// Test_OperatorPredicates_LabelChange_Reconciled — label mutation
// triggers reconcile (e.g., runtime.getloopscale.com/managed-by, app.kubernetes.io/name
// during chart upgrade).
func Test_OperatorPredicates_LabelChange_Reconciled(t *testing.T) {
	p := OperatorPredicates()

	old := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Generation: 1,
			Labels:     map[string]string{"l": "v"},
		},
	}
	updated := old.DeepCopy()
	updated.Labels["runtime.getloopscale.com/cell-id"] = "cell-0"

	if !p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: updated}) {
		t.Errorf("OperatorPredicates.Update returned false on label add; expected true")
	}
}

// Test_OperatorPredicates_Create_Always — Create events MUST always
// reconcile. New objects need their initial reconcile pass.
func Test_OperatorPredicates_Create_Always(t *testing.T) {
	p := OperatorPredicates()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "new", Namespace: "default"},
	}
	if !p.Create(event.CreateEvent{Object: cm}) {
		t.Errorf("OperatorPredicates.Create returned false; expected true (initial reconcile required)")
	}
}

// Test_OperatorPredicates_Delete_Always — Delete events MUST always
// reconcile (finalizer cleanup paths).
func Test_OperatorPredicates_Delete_Always(t *testing.T) {
	p := OperatorPredicates()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: "default"},
	}
	if !p.Delete(event.DeleteEvent{Object: cm}) {
		t.Errorf("OperatorPredicates.Delete returned false; expected true (finalizer cleanup)")
	}
}
