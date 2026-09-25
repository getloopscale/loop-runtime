// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// loopzero-api-tokens has several writers: the gitea-token seed (gitea-token), the
// argocd-token seed (argocd-token) and External Secrets (cf-api-token, charts
// loopzero-api values-bootstrap). A seed that replaces the whole Secret erases the
// keys it does not own; each must write only its own key.
func existingAPITokens() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loopzero-system", Name: "loopzero-api-tokens"},
		Data: map[string][]byte{
			"gitea-token":  []byte("old-gitea"),
			"argocd-token": []byte("old-argocd"),
			"cf-api-token": []byte("cf-from-eso"),
		},
	}
}

func TestGiteaToken_KeepsTheOtherKeysOfLoopZeroAPITokens(t *testing.T) {
	server, _ := stubGiteaServer(t, true)
	defer server.Close()
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(giteaAdminWithCreds("gitea_admin", "pw", "old"), existingAPITokens(), makeLoopZeroAPIDeployment()).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), PortForwardCtor: pfCtorPointingAt(server)}

	if got := NewGiteaTokenSeed().Run(deps); got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}

	var s corev1.Secret
	if err := c.Get(context.Background(), fakeKey("loopzero-system", "loopzero-api-tokens"), &s); err != nil {
		t.Fatal(err)
	}
	if string(s.Data["gitea-token"]) != "canonical-gitea-sha1" {
		t.Errorf("gitea-token = %q, want the minted token", s.Data["gitea-token"])
	}
	for key, want := range map[string]string{"argocd-token": "old-argocd", "cf-api-token": "cf-from-eso"} {
		if string(s.Data[key]) != want {
			t.Errorf("%s = %q after the gitea seed, want %q (another writer's key)", key, s.Data[key], want)
		}
	}
}

func TestArgoCDToken_KeepsTheOtherKeysOfLoopZeroAPITokens(t *testing.T) {
	server := stubArgoCDServer(t)
	defer server.Close()
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(argocdInitialAdminSecretFixture("adminpw"), existingAPITokens(), makeLoopZeroAPIDeployment()).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), PortForwardCtor: pfCtorPointingAt(server)}

	if got := NewArgoCDTokenSeed().Run(deps); got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}

	var s corev1.Secret
	if err := c.Get(context.Background(), fakeKey("loopzero-system", "loopzero-api-tokens"), &s); err != nil {
		t.Fatal(err)
	}
	if string(s.Data["argocd-token"]) != "canonical-account-token" {
		t.Errorf("argocd-token = %q, want the minted token", s.Data["argocd-token"])
	}
	for key, want := range map[string]string{"gitea-token": "old-gitea", "cf-api-token": "cf-from-eso"} {
		if string(s.Data[key]) != want {
			t.Errorf("%s = %q after the argocd seed, want %q (another writer's key)", key, s.Data[key], want)
		}
	}
}

// The bootstrap chain provisions tokens before loopzero-api is necessarily
// deployed; the token is read when the API starts, so an absent Deployment has
// nothing to restart. Its sibling gitea-token seed already treats it that way.
func TestArgoCDToken_PassesWhenLoopZeroAPIIsNotDeployedYet(t *testing.T) {
	server := stubArgoCDServer(t)
	defer server.Close()
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(argocdInitialAdminSecretFixture("adminpw")).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), PortForwardCtor: pfCtorPointingAt(server)}

	got := NewArgoCDTokenSeed().Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	var s corev1.Secret
	if err := c.Get(context.Background(), fakeKey("loopzero-system", "loopzero-api-tokens"), &s); err != nil {
		t.Fatalf("loopzero-api-tokens not written: %v", err)
	}
}
