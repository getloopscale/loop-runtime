// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("add corev1: %v", err)
	}
	if err := appsv1.AddToScheme(s); err != nil {
		t.Fatalf("add appsv1: %v", err)
	}
	return s
}

func TestCreateOrUpdateSecret_CreatesWhenAbsent(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	k := NewKubeClient(c)
	created, err := k.CreateOrUpdateSecret(context.Background(),
		secretOf("loopzero-system", "s1", map[string][]byte{"k": []byte("v")}))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !created {
		t.Error("created = false, want true (Secret was absent)")
	}
	got, err := k.GetSecret(context.Background(), "loopzero-system", "s1")
	if err != nil {
		t.Fatalf("get after create: %v", err)
	}
	if string(got.Data["k"]) != "v" {
		t.Errorf("Data[k] = %q, want v", got.Data["k"])
	}
}

func TestCreateOrUpdateSecret_UpdatesWhenExists(t *testing.T) {
	existing := secretOf("loopzero-system", "s1", map[string][]byte{"k": []byte("old")})
	existing.ResourceVersion = "" // fake client assigns RV on create
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(existing).Build()
	k := NewKubeClient(c)
	created, err := k.CreateOrUpdateSecret(context.Background(),
		secretOf("loopzero-system", "s1", map[string][]byte{"k": []byte("new")}))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if created {
		t.Error("created = true, want false (Secret already existed)")
	}
	got, _ := k.GetSecret(context.Background(), "loopzero-system", "s1")
	if string(got.Data["k"]) != "new" {
		t.Errorf("Data[k] = %q, want new (update applied)", got.Data["k"])
	}
}

func TestWaitDeploymentReady_ReturnsWhenReady(t *testing.T) {
	replicas := int32(2)
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loopzero-system", Name: "loopzero-api"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: 2},
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(d).Build()
	k := NewKubeClient(c)
	if err := k.WaitDeploymentReady(context.Background(), "loopzero-system", "loopzero-api", 3*time.Second); err != nil {
		t.Errorf("ready deployment: unexpected err %v", err)
	}
}

func TestWaitDeploymentReady_TimesOutWhenNeverReady(t *testing.T) {
	replicas := int32(2)
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loopzero-system", Name: "loopzero-api"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: 0}, // never ready
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(d).Build()
	k := NewKubeClient(c)
	err := k.WaitDeploymentReady(context.Background(), "loopzero-system", "loopzero-api", 1*time.Second)
	if err == nil {
		t.Error("never-ready deployment: want timeout error, got nil")
	}
}
