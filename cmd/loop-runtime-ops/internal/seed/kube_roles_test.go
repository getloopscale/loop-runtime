// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// The operations below used to reach through KubeClient to the concrete
// *kubeClient, and silently did nothing for any other implementation. They are
// part of the port now, so their behaviour is observable through it.

func TestEnsureNamespace_CreatesAnAbsentNamespace(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	if err := NewKubeClient(c).EnsureNamespace(context.Background(), "loop-runtime-system"); err != nil {
		t.Fatalf("EnsureNamespace: %v", err)
	}
	var ns corev1.Namespace
	if err := c.Get(context.Background(), fakeKey("", "loop-runtime-system"), &ns); err != nil {
		t.Fatalf("namespace was not created: %v", err)
	}
}

func TestEnsureNamespace_IsIdempotent(t *testing.T) {
	existing := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "loop-runtime-system"}}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(existing).Build()
	if err := NewKubeClient(c).EnsureNamespace(context.Background(), "loop-runtime-system"); err != nil {
		t.Fatalf("EnsureNamespace on an existing namespace: %v", err)
	}
}

func TestRolloutRestartDaemonSet_StampsTheTemplate(t *testing.T) {
	ds := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Namespace: "monitoring", Name: "promtail"}}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(ds).Build()
	if err := NewKubeClient(c).RolloutRestartDaemonSet(context.Background(), "monitoring", "promtail"); err != nil {
		t.Fatalf("RolloutRestartDaemonSet: %v", err)
	}
	var got appsv1.DaemonSet
	_ = c.Get(context.Background(), fakeKey("monitoring", "promtail"), &got)
	if got.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Error("the pod template carries no restartedAt stamp, so no rollout was triggered")
	}
}

func TestGetService_ReturnsTheServiceAndPropagatesNotFound(t *testing.T) {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "secrets-system", Name: "openbao"}}
	k := NewKubeClient(fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(svc).Build())
	if _, err := k.GetService(context.Background(), "secrets-system", "openbao"); err != nil {
		t.Fatalf("GetService: %v", err)
	}
	if _, err := k.GetService(context.Background(), "secrets-system", "absent"); err == nil {
		t.Error("an absent Service must be an error, not a zero value")
	}
}
