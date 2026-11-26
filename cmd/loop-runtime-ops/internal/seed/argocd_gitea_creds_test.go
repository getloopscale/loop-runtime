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

// giteaAdminSecret builds a mgmt/loop-runtime-system/gitea-admin fixture Secret.
func giteaAdminSecret(user, token string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loop-runtime-system", Name: "gitea-admin"},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{"username": []byte(user), "token": []byte(token)},
	}
}

func TestArgoCDGiteaCreds_CreatesRepoCreds(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(giteaAdminSecret("loop-runtime-admin", "gta_realtoken")).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}

	got := NewArgoCDGiteaCredsSeed().Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	var s corev1.Secret
	if err := c.Get(context.Background(), fakeKey("argocd", "argocd-creds-gitea"), &s); err != nil {
		t.Fatalf("argocd-creds-gitea not created: %v", err)
	}
	if s.Labels["argocd.argoproj.io/secret-type"] != "repo-creds" {
		t.Errorf("secret-type label missing/wrong: %q", s.Labels["argocd.argoproj.io/secret-type"])
	}
	if string(s.Data["username"]) != "loop-runtime-admin" {
		t.Errorf("username = %q, want loop-runtime-admin", s.Data["username"])
	}
	if string(s.Data["password"]) != "gta_realtoken" {
		t.Errorf("password = %q, want gta_realtoken", s.Data["password"])
	}
	if string(s.Data["type"]) != "git" {
		t.Errorf("type = %q, want git", s.Data["type"])
	}
	if string(s.Data["url"]) != "http://gitea-http.loop-runtime-system.svc.cluster.local:3000/loop-runtime" {
		t.Errorf("url mismatch: %q", s.Data["url"])
	}
}

func TestArgoCDGiteaCreds_NoopWhenConverged(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(giteaAdminSecret("u", "t")).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}
	seed := NewArgoCDGiteaCredsSeed()

	if r := seed.Run(deps); r.Verdict != PASS {
		t.Fatalf("first: %s (%s)", r.Verdict, r.Reason)
	}
	if r := seed.Run(deps); r.Verdict != PASS {
		t.Fatalf("second (converged): %s (%s)", r.Verdict, r.Reason)
	}
}

func TestArgoCDGiteaCreds_FailWhenGiteaAdminMissing(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build() // no gitea-admin
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}

	got := NewArgoCDGiteaCredsSeed().Run(deps)

	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL (gitea-admin absent)", got.Verdict, got.Reason)
	}
}

func TestArgoCDGiteaCreds_FailWhenTokenEmpty(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(giteaAdminSecret("loop-runtime-admin", "")).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}

	got := NewArgoCDGiteaCredsSeed().Run(deps)

	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL (token empty)", got.Verdict, got.Reason)
	}
}

func TestArgoCDGiteaCreds_FailWhenUsernameEmpty(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(giteaAdminSecret("", "gta_token")).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}

	got := NewArgoCDGiteaCredsSeed().Run(deps)

	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL (username empty)", got.Verdict, got.Reason)
	}
}

func TestArgoCDGiteaCreds_VerifyPassesAfterRun(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(giteaAdminSecret("u", "t")).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}
	seed := NewArgoCDGiteaCredsSeed()

	seed.Run(deps)
	got := seed.Verify(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verify = %s (%s), want PASS", got.Verdict, got.Reason)
	}
}

func TestArgoCDGiteaCreds_VerifyFailsOnDrift(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(giteaAdminSecret("u", "t")).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c)}
	seed := NewArgoCDGiteaCredsSeed()
	seed.Run(deps)

	// Simulate drift: gitea-admin token rotated but repo-creds not re-run.
	got := giteaAdminSecret("u", "new-token")
	if err := c.Update(context.Background(), got); err != nil {
		t.Fatalf("update gitea-admin: %v", err)
	}
	verdict := seed.Verify(deps)

	if verdict.Verdict != FAIL {
		t.Fatalf("Verify with drift = %s (%s), want FAIL", verdict.Verdict, verdict.Reason)
	}
}

func TestArgoCDGiteaCreds_Name(t *testing.T) {
	if got := NewArgoCDGiteaCredsSeed().Name(); got != "argocd-gitea-creds" {
		t.Errorf("Name = %q, want argocd-gitea-creds", got)
	}
}
