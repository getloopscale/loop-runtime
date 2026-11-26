// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Measured live 2026-09-30 (#470): right after a restart of a one-replica Deployment the
// controller reports updatedReplicas=1 and readyReplicas=1 while the new pod is still
// starting — the ready one is the OLD pod the surge has not removed yet. The wait
// returned and the seed reported loopruntime-cloud rolled with the new pod at 0/1. Until the old
// pod is gone the Deployment holds more pods than it wants.
func TestWaitForRolloutSettledWaitsForTheSurgePodToLeave(t *testing.T) {
	one := int32(1)
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loopruntime-system", Name: "loopruntime-cloud"},
		Spec:       appsv1.DeploymentSpec{Replicas: &one},
		Status:     appsv1.DeploymentStatus{Replicas: 2, UpdatedReplicas: 1, ReadyReplicas: 1},
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(d).Build()

	err := waitForRolloutSettled(context.Background(), c, "loopruntime-system", "loopruntime-cloud", 50*time.Millisecond)

	if err == nil {
		t.Fatal("settled while the old pod was still counted — the new one may not be ready")
	}
}

func TestWaitForRolloutSettledReturnsOnceOnlyTheNewPodsRemain(t *testing.T) {
	one := int32(1)
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loopruntime-system", Name: "loopruntime-cloud"},
		Spec:       appsv1.DeploymentSpec{Replicas: &one},
		Status:     appsv1.DeploymentStatus{Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1},
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(d).Build()

	if err := waitForRolloutSettled(context.Background(), c, "loopruntime-system", "loopruntime-cloud", time.Second); err != nil {
		t.Fatalf("did not settle: %v", err)
	}
}
