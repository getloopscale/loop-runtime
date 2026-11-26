// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"context"
	"testing"

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

// makeLoopRuntimeAPIDeploymentWithEnv builds a Deployment with N env vars.
func makeLoopRuntimeAPIDeploymentWithEnv(envs []corev1.EnvVar) *appsv1.Deployment {
	one := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loopruntime-system", Name: "loopruntime-api"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &one,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "api", Env: envs}}},
			},
		},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 1, UpdatedReplicas: 1},
	}
}

func TestCreateOrUpdateSecret_CreatesWhenAbsent(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	k := NewKubeClient(c)
	created, err := k.CreateOrUpdateSecret(context.Background(),
		secretOf("loopruntime-system", "s1", map[string][]byte{"k": []byte("v")}))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !created {
		t.Error("created = false, want true (Secret was absent)")
	}
	got, err := k.GetSecret(context.Background(), "loopruntime-system", "s1")
	if err != nil {
		t.Fatalf("get after create: %v", err)
	}
	if string(got.Data["k"]) != "v" {
		t.Errorf("Data[k] = %q, want v", got.Data["k"])
	}
}

func TestCreateOrUpdateSecret_UpdatesWhenExists(t *testing.T) {
	existing := secretOf("loopruntime-system", "s1", map[string][]byte{"k": []byte("old")})
	existing.ResourceVersion = "" // fake client assigns RV on create
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(existing).Build()
	k := NewKubeClient(c)
	created, err := k.CreateOrUpdateSecret(context.Background(),
		secretOf("loopruntime-system", "s1", map[string][]byte{"k": []byte("new")}))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if created {
		t.Error("created = true, want false (Secret already existed)")
	}
	got, _ := k.GetSecret(context.Background(), "loopruntime-system", "s1")
	if string(got.Data["k"]) != "new" {
		t.Errorf("Data[k] = %q, want new (update applied)", got.Data["k"])
	}
}
