// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// makeLoopZeroAPIDeploymentWithEnv builds a Deployment with N env vars.
func makeLoopZeroAPIDeploymentWithEnv(envs []corev1.EnvVar) *appsv1.Deployment {
	one := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loopzero-system", Name: "loopzero-api"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &one,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "api", Env: envs}}},
			},
		},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 1, UpdatedReplicas: 1},
	}
}

func TestSetDeploymentContainerEnv_UpsertsExisting(t *testing.T) {
	d := makeLoopZeroAPIDeploymentWithEnv([]corev1.EnvVar{
		{Name: "OPENBAO_VAULT_ADDR", Value: "https://stale:1234"},
		{Name: "UNRELATED", Value: "keep"},
	})
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(d).Build()

	if err := setDeploymentContainerEnv(context.Background(), c,
		"loopzero-system", "loopzero-api", "OPENBAO_VAULT_ADDR", "https://fresh:5000"); err != nil {
		t.Fatalf("setDeploymentContainerEnv: %v", err)
	}
	got, err := getDeploymentContainerEnv(context.Background(), c,
		"loopzero-system", "loopzero-api", "OPENBAO_VAULT_ADDR")
	if err != nil {
		t.Fatalf("get env: %v", err)
	}
	if got != "https://fresh:5000" {
		t.Errorf("env upsert failed: got %q", got)
	}
	// Assert unrelated env preserved.
	other, _ := getDeploymentContainerEnv(context.Background(), c, "loopzero-system", "loopzero-api", "UNRELATED")
	if other != "keep" {
		t.Errorf("unrelated env clobbered: %q", other)
	}
}

func TestSetDeploymentContainerEnv_AppendsNew(t *testing.T) {
	d := makeLoopZeroAPIDeploymentWithEnv([]corev1.EnvVar{{Name: "OTHER", Value: "v"}})
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(d).Build()

	_ = setDeploymentContainerEnv(context.Background(), c,
		"loopzero-system", "loopzero-api", "OPENBAO_VAULT_ADDR", "https://x:1")

	got, _ := getDeploymentContainerEnv(context.Background(), c,
		"loopzero-system", "loopzero-api", "OPENBAO_VAULT_ADDR")
	if got != "https://x:1" {
		t.Errorf("env not appended: got %q", got)
	}
}

func TestSetDeploymentContainerEnv_BumpsRestartAnnotation(t *testing.T) {
	d := makeLoopZeroAPIDeploymentWithEnv(nil)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(d).Build()

	_ = setDeploymentContainerEnv(context.Background(), c,
		"loopzero-system", "loopzero-api", "K", "V")

	var got appsv1.Deployment
	_ = c.Get(context.Background(), fakeKey("loopzero-system", "loopzero-api"), &got)
	if got.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Error("restartedAt annotation not bumped")
	}
}

func TestVaultServiceHTTPSNodePort_ReturnsPort(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(makeVaultService(31234)).Build()
	k := NewKubeClient(c)

	np, err := vaultServiceHTTPSNodePort(context.Background(), k)
	if err != nil {
		t.Fatalf("nodeport: %v", err)
	}
	if np != 31234 {
		t.Errorf("np = %d, want 31234", np)
	}
}

func TestVaultServiceHTTPSNodePort_FailsWhenNodePortZero(t *testing.T) {
	svc := makeVaultService(0)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(svc).Build()
	k := NewKubeClient(c)

	_, err := vaultServiceHTTPSNodePort(context.Background(), k)
	if err == nil || !strings.Contains(err.Error(), "NodePort") {
		t.Errorf("want NodePort error, got %v", err)
	}
}

func TestVaultCrossClusterAddr_HappyPath_SetsEnv(t *testing.T) {
	var host Host
	// Fake docker inspect returns a canned IP.
	host.DockerContainerIP = func(container string) (string, error) { return "10.99.0.42", nil }

	// mgmt cluster: has loopzero-api Deployment.
	mgmt := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(makeLoopZeroAPIDeploymentWithEnv(nil)).Build()
	// vault cluster: has openbao Service with NodePort 30443.
	vault := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(makeVaultService(30443)).Build()

	deps := Deps{Host: host,
		RolloutSettleTimeout: time.Millisecond,
		Ctx:                  context.Background(),
		Kube:                 NewKubeClient(mgmt),
		CtrlClient:           mgmt,
		Clients:              map[string]KubeClient{vaultContextName: NewKubeClient(vault)},
	}
	got := NewVaultCrossClusterAddrSeed().Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	value, _ := getDeploymentContainerEnv(context.Background(), mgmt,
		"loopzero-system", "loopzero-api", "OPENBAO_VAULT_ADDR")
	if value != "https://10.99.0.42:30443" {
		t.Errorf("OPENBAO_VAULT_ADDR = %q, want https://10.99.0.42:30443", value)
	}
}

func TestVaultCrossClusterAddr_FailWhenVaultServiceMissing(t *testing.T) {
	var host Host
	host.DockerContainerIP = func(container string) (string, error) { return "10.0.0.1", nil }

	mgmt := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	vault := fake.NewClientBuilder().WithScheme(newScheme(t)).Build() // no Service
	deps := Deps{Host: host,
		RolloutSettleTimeout: time.Millisecond,
		Ctx:                  context.Background(),
		Kube:                 NewKubeClient(mgmt),
		CtrlClient:           mgmt,
		Clients:              map[string]KubeClient{vaultContextName: NewKubeClient(vault)},
	}

	got := NewVaultCrossClusterAddrSeed().Run(deps)

	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
}

func TestVaultCrossClusterAddr_Name(t *testing.T) {
	if got := NewVaultCrossClusterAddrSeed().Name(); got != "vault-cross-cluster-addr" {
		t.Errorf("Name = %q, want vault-cross-cluster-addr", got)
	}
}
