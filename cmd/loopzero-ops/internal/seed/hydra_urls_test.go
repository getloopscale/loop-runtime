// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// sampleHydraYAML is a small chart-default hydra.yaml fragment covering the
// URL keys the seed rewrites. Whitespace + indentation preserved.
const sampleHydraYAML = `
urls:
  self:
    issuer: http://ory-hydra-public.loopzero-auth.svc.cluster.local:4444/
    public: http://ory-hydra-public.loopzero-auth.svc.cluster.local:4444/
  device:
    verification: http://localhost:4455/device-verify
    success: http://localhost:4455/device-success
  login: http://localhost:3000/auth/login
  consent: http://localhost:3000/auth/consent
  logout: http://localhost:3000/auth/logout
  error: http://localhost:3000/auth/error
`

func makeHydraCM(yaml string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loopzero-auth", Name: "ory-hydra"},
		Data:       map[string]string{"hydra.yaml": yaml},
	}
}

func makeHydraDeployment() *appsv1.Deployment {
	one := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loopzero-auth", Name: "ory-hydra"},
		Spec:       appsv1.DeploymentSpec{Replicas: &one},
	}
}

func TestHydraURLs_RewritesAllKeys(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(makeHydraCM(sampleHydraYAML), makeHydraDeployment()).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}

	got := NewHydraURLsSeed().Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	var cm corev1.ConfigMap
	if err := c.Get(context.Background(), fakeKey("loopzero-auth", "ory-hydra"), &cm); err != nil {
		t.Fatalf("get cm: %v", err)
	}
	updated := cm.Data["hydra.yaml"]
	for _, want := range []string{
		"issuer: https://auth-dev.loopzero.example/",
		"public: https://auth-dev.loopzero.example/",
		"verification: https://app-dev.loopzero.example/device-verify",
		"success: https://app-dev.loopzero.example/device-success",
		"login: https://app-dev.loopzero.example/auth/login",
		"consent: https://app-dev.loopzero.example/auth/consent",
		"logout: https://app-dev.loopzero.example/logout",
		"error: https://app-dev.loopzero.example/auth/error",
	} {
		if !strings.Contains(updated, want) {
			t.Errorf("updated hydra.yaml missing %q\nGot:\n%s", want, updated)
		}
	}
	// Assert rollout annotation set on the Deployment.
	var d appsv1.Deployment
	_ = c.Get(context.Background(), fakeKey("loopzero-auth", "ory-hydra"), &d)
	if d.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Error("restartedAt annotation not bumped")
	}
}

func TestHydraURLs_NoopWhenAlreadyCanonical(t *testing.T) {
	canonical := rewriteHydraURLs(sampleHydraYAML, "auth-dev.loopzero.example", "app-dev.loopzero.example")
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(makeHydraCM(canonical), makeHydraDeployment()).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}

	got := NewHydraURLsSeed().Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	if !strings.Contains(got.Reason, "no-op") {
		t.Errorf("Reason = %q, want 'no-op'", got.Reason)
	}
}

func TestHydraURLs_FailWhenConfigMapMissing(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}

	got := NewHydraURLsSeed().Run(deps)

	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
}

func TestHydraURLs_HonoursEnvOverride(t *testing.T) {
	var host Host
	host.Getenv = func(k string) string {
		switch k {
		case "LOOPZERO_AUTH_HOST":
			return "custom-auth.example.com"
		case "LOOPZERO_APP_HOST":
			return "custom-app.example.com"
		}
		return ""
	}

	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(makeHydraCM(sampleHydraYAML), makeHydraDeployment()).Build()
	deps := Deps{Host: host, Ctx: context.Background(), Kube: NewKubeClient(c)}

	NewHydraURLsSeed().Run(deps)

	var cm corev1.ConfigMap
	_ = c.Get(context.Background(), fakeKey("loopzero-auth", "ory-hydra"), &cm)
	if !strings.Contains(cm.Data["hydra.yaml"], "issuer: https://custom-auth.example.com/") {
		t.Errorf("env override not applied to issuer")
	}
}

func TestHydraURLs_VerifyPassesWhenCanonical(t *testing.T) {
	canonical := rewriteHydraURLs(sampleHydraYAML, "auth-dev.loopzero.example", "app-dev.loopzero.example")
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(makeHydraCM(canonical)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}

	if got := NewHydraURLsSeed().Verify(deps); got.Verdict != PASS {
		t.Fatalf("Verify = %s (%s), want PASS", got.Verdict, got.Reason)
	}
}

func TestHydraURLs_VerifyFailsOnDrift(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(makeHydraCM(sampleHydraYAML)).Build() // NOT canonical
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}

	if got := NewHydraURLsSeed().Verify(deps); got.Verdict != FAIL {
		t.Fatalf("Verify (drifted) = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
}

func TestHydraURLs_Name(t *testing.T) {
	if got := NewHydraURLsSeed().Name(); got != "hydra-urls" {
		t.Errorf("Name = %q, want hydra-urls", got)
	}
}
