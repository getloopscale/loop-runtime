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

func minioMainSecretObj(rootPassword string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loop-runtime-system", Name: "minio"},
		Data:       map[string][]byte{"rootPassword": []byte(rootPassword)},
	}
}

func makeMinioDeployment() *appsv1.Deployment {
	one := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loop-runtime-system", Name: "minio"},
		Spec:       appsv1.DeploymentSpec{Replicas: &one},
	}
}

func makeLoopRuntimeAPIDeployment() *appsv1.Deployment {
	one := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loop-runtime-system", Name: "loop-runtime-api"},
		Spec:       appsv1.DeploymentSpec{Replicas: &one},
	}
}

func TestMinioCanonical_SyncsMgmtPasswordAndRestarts(t *testing.T) {
	mgmt := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(
			minioMainSecretObj("random-auto-generated"),
			makeMinioDeployment(),
			makeLoopRuntimeAPIDeployment(),
		).Build()
	// No build cluster secret — Layer 3 must SKIP-graceful.
	buildCli := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{
		Ctx:     context.Background(),
		Kube:    NewKubeClient(mgmt),
		Clients: map[string]KubeClient{"k3d-loop-runtime-build": NewKubeClient(buildCli)},
	}

	got := NewMinioCanonicalSeed().Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	// Assert password was rewritten.
	var s corev1.Secret
	if err := mgmt.Get(context.Background(), fakeKey("loop-runtime-system", "minio"), &s); err != nil {
		t.Fatalf("get minio Secret: %v", err)
	}
	if string(s.Data["rootPassword"]) != "loop-runtime-dev-password" {
		t.Errorf("rootPassword = %q, want loop-runtime-dev-password", s.Data["rootPassword"])
	}
	// Assert both Deployments rolled.
	for _, name := range []string{"minio", "loop-runtime-api"} {
		var d appsv1.Deployment
		if err := mgmt.Get(context.Background(), fakeKey("loop-runtime-system", name), &d); err != nil {
			t.Fatalf("get %s: %v", name, err)
		}
		if d.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] == "" {
			t.Errorf("%s: restartedAt annotation not bumped", name)
		}
	}
}

func TestMinioCanonical_PatchesBuildClusterSecretWhenPresent(t *testing.T) {
	mgmt := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(
			minioMainSecretObj("random"),
			makeMinioDeployment(),
			makeLoopRuntimeAPIDeployment(),
		).Build()
	// Build cluster has the loop-runtime-minio-credentials Secret to patch.
	buildSec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "argo", Name: "loop-runtime-minio-credentials"},
		Data:       map[string][]byte{"AWS_ACCESS_KEY_ID": []byte("loop-runtime-admin"), "AWS_SECRET_ACCESS_KEY": []byte("stale")},
	}
	buildCli := fake.NewClientBuilder().WithScheme(newScheme(t)).WithObjects(buildSec).Build()
	deps := Deps{
		Ctx:     context.Background(),
		Kube:    NewKubeClient(mgmt),
		Clients: map[string]KubeClient{"k3d-loop-runtime-build": NewKubeClient(buildCli)},
	}

	if got := NewMinioCanonicalSeed().Run(deps); got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	var s corev1.Secret
	if err := buildCli.Get(context.Background(), fakeKey("argo", "loop-runtime-minio-credentials"), &s); err != nil {
		t.Fatalf("get build secret: %v", err)
	}
	if string(s.Data["AWS_SECRET_ACCESS_KEY"]) != "loop-runtime-dev-password" {
		t.Errorf("AWS_SECRET_ACCESS_KEY = %q, want loop-runtime-dev-password", s.Data["AWS_SECRET_ACCESS_KEY"])
	}
	// Assert unrelated key preserved (Layer 3 must not clobber siblings).
	if string(s.Data["AWS_ACCESS_KEY_ID"]) != "loop-runtime-admin" {
		t.Errorf("AWS_ACCESS_KEY_ID = %q, want loop-runtime-admin (preserved)", s.Data["AWS_ACCESS_KEY_ID"])
	}
}

func TestMinioCanonical_PatchesLegacyAliasWhenPresent(t *testing.T) {
	legacy := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loop-runtime-system", Name: "minio-credentials"},
		Data:       map[string][]byte{"secretkey": []byte("stale"), "accesskey": []byte("loop-runtime")},
	}
	mgmt := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(minioMainSecretObj("r"), legacy, makeMinioDeployment(), makeLoopRuntimeAPIDeployment()).Build()
	buildCli := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{
		Ctx:     context.Background(),
		Kube:    NewKubeClient(mgmt),
		Clients: map[string]KubeClient{"k3d-loop-runtime-build": NewKubeClient(buildCli)},
	}

	NewMinioCanonicalSeed().Run(deps)

	var s corev1.Secret
	if err := mgmt.Get(context.Background(), fakeKey("loop-runtime-system", "minio-credentials"), &s); err != nil {
		t.Fatalf("get legacy alias: %v", err)
	}
	if string(s.Data["secretkey"]) != "loop-runtime-dev-password" {
		t.Errorf("legacy secretkey = %q, want loop-runtime-dev-password", s.Data["secretkey"])
	}
	if string(s.Data["accesskey"]) != "loop-runtime" {
		t.Errorf("accesskey clobbered: %q", s.Data["accesskey"])
	}
}

func TestMinioCanonical_FailWhenMinioSecretMissing(t *testing.T) {
	mgmt := fake.NewClientBuilder().WithScheme(newScheme(t)).Build() // no minio Secret
	buildCli := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{
		Ctx:     context.Background(),
		Kube:    NewKubeClient(mgmt),
		Clients: map[string]KubeClient{"k3d-loop-runtime-build": NewKubeClient(buildCli)},
	}

	got := NewMinioCanonicalSeed().Run(deps)

	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL (minio Secret absent)", got.Verdict, got.Reason)
	}
}

func TestMinioCanonical_VerifyPassesAfterRun(t *testing.T) {
	mgmt := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(minioMainSecretObj("r"), makeMinioDeployment(), makeLoopRuntimeAPIDeployment()).Build()
	buildCli := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{
		Ctx:     context.Background(),
		Kube:    NewKubeClient(mgmt),
		Clients: map[string]KubeClient{"k3d-loop-runtime-build": NewKubeClient(buildCli)},
	}
	seed := NewMinioCanonicalSeed()

	seed.Run(deps)
	got := seed.Verify(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verify = %s (%s), want PASS", got.Verdict, got.Reason)
	}
}

func TestMinioCanonical_HonoursEnvOverride(t *testing.T) {
	var host Host
	host.Getenv = func(k string) string {
		if k == "LOOP_RUNTIME_MINIO_ROOT_PASSWORD" {
			return "operator-override-pw"
		}
		return ""
	}

	mgmt := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(minioMainSecretObj("r"), makeMinioDeployment(), makeLoopRuntimeAPIDeployment()).Build()
	buildCli := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Host: host,
		Ctx:     context.Background(),
		Kube:    NewKubeClient(mgmt),
		Clients: map[string]KubeClient{"k3d-loop-runtime-build": NewKubeClient(buildCli)},
	}

	NewMinioCanonicalSeed().Run(deps)

	var s corev1.Secret
	_ = mgmt.Get(context.Background(), fakeKey("loop-runtime-system", "minio"), &s)
	if string(s.Data["rootPassword"]) != "operator-override-pw" {
		t.Errorf("rootPassword = %q, want operator-override-pw", s.Data["rootPassword"])
	}
}

func TestMinioCanonical_Name(t *testing.T) {
	if got := NewMinioCanonicalSeed().Name(); got != "minio" {
		t.Errorf("Name = %q, want minio", got)
	}
}
