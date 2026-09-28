// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func writeStripeAPICreds(t *testing.T, apiKey, starter, pro, team string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "dev-credentials.json")
	content := `{"stripe":{"api_key":` + mustJSONString(apiKey) +
		`,"price_starter":` + mustJSONString(starter) +
		`,"price_pro":` + mustJSONString(pro) +
		`,"price_team":` + mustJSONString(team) + `}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write creds: %v", err)
	}
	return path
}

// makeLoopZeroCloudDeployment is a helper that seeds a fake loopzero-cloud Deployment
// so RolloutRestartDeployment (the post-apply side effect) succeeds.
func makeLoopZeroCloudDeployment() *appsv1.Deployment {
	one := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "loopzero-system", Name: "loopzero-cloud"},
		Spec:       appsv1.DeploymentSpec{Replicas: &one},
	}
}

func TestStripeAPIKeys_CreatesSecretAndRestarts(t *testing.T) {
	credsPath := writeStripeAPICreds(t, "rk_test_realkey", "price_starter_x", "price_pro_x", "price_team_x")
	creds, err := LoadCreds(credsPath)
	if err != nil {
		t.Fatalf("LoadCreds: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(makeLoopZeroCloudDeployment()).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: creds}

	got := NewStripeAPIKeysSeed().Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	var s corev1.Secret
	if err := c.Get(context.Background(), fakeKey("loopzero-system", "loopzero-stripe-api-keys"), &s); err != nil {
		t.Fatalf("Secret not created: %v", err)
	}
	if string(s.Data["STRIPE_API_KEY"]) != "rk_test_realkey" {
		t.Errorf("STRIPE_API_KEY = %q, want rk_test_realkey", s.Data["STRIPE_API_KEY"])
	}
	if string(s.Data["STRIPE_PRICE_PRO"]) != "price_pro_x" {
		t.Errorf("STRIPE_PRICE_PRO = %q, want price_pro_x", s.Data["STRIPE_PRICE_PRO"])
	}
	// Verify rollout annotation set on the Deployment.
	var d appsv1.Deployment
	if err := c.Get(context.Background(), fakeKey("loopzero-system", "loopzero-cloud"), &d); err != nil {
		t.Fatalf("loopzero-cloud Deployment lookup: %v", err)
	}
	if d.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Error("restartedAt annotation not set (rollout restart did not fire)")
	}
}

func TestStripeAPIKeys_SkipWhenCredsAbsent(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: nil}

	got := NewStripeAPIKeysSeed().Run(deps)

	if got.Verdict != SKIP {
		t.Fatalf("Verdict = %s (%s), want SKIP", got.Verdict, got.Reason)
	}
}

func TestStripeAPIKeys_SkipWhenPlaceholderAndBillingNotRequired(t *testing.T) {
	for _, ph := range []string{"sk_test_REPLACE_ME", "REPLACE_VIA_PULUMI", "sk_live_PLACEHOLDER"} {
		t.Run(ph, func(t *testing.T) {
			credsPath := writeStripeAPICreds(t, ph, "", "", "")
			creds, _ := LoadCreds(credsPath)
			c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
			deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: creds}
			if got := NewStripeAPIKeysSeed().Run(deps); got.Verdict != SKIP {
				t.Errorf("Verdict = %s (%s), want SKIP", got.Verdict, got.Reason)
			}
		})
	}
}

func TestStripeAPIKeys_FailWhenWrongPrefix(t *testing.T) {
	// Publishable key pasted into api_key field — EC-2 guard MUST catch.
	credsPath := writeStripeAPICreds(t, "pk_test_publishable", "", "", "")
	creds, _ := LoadCreds(credsPath)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: creds}

	got := NewStripeAPIKeysSeed().Run(deps)

	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL (pk_* prefix)", got.Verdict, got.Reason)
	}
	// No Secret should have been applied on FAIL.
	var s corev1.Secret
	if err := c.Get(context.Background(), fakeKey("loopzero-system", "loopzero-stripe-api-keys"), &s); err == nil {
		t.Error("Secret applied despite FAIL (fail-fast violated)")
	}
}

func TestStripeAPIKeys_FailsWhenCloudDeploymentAbsent(t *testing.T) {
	// D3 byte-parity with the bash seed: rollout NotFound → FAIL (bash exit 1).
	// Softer contract deferred to follow-up ADR.
	credsPath := writeStripeAPICreds(t, "rk_test_x", "", "", "")
	creds, _ := LoadCreds(credsPath)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build() // no Deployment
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: creds}

	got := NewStripeAPIKeysSeed().Run(deps)

	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL (bash parity — Deployment NotFound)", got.Verdict, got.Reason)
	}
}

func TestStripeAPIKeys_VerifyPassesAfterRun(t *testing.T) {
	credsPath := writeStripeAPICreds(t, "sk_live_prod", "", "", "")
	creds, _ := LoadCreds(credsPath)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).
		WithObjects(makeLoopZeroCloudDeployment()).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: creds}
	seed := NewStripeAPIKeysSeed()

	seed.Run(deps)
	got := seed.Verify(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verify = %s (%s), want PASS", got.Verdict, got.Reason)
	}
}

func TestStripeAPIKeys_Name(t *testing.T) {
	if got := NewStripeAPIKeysSeed().Name(); got != "stripe-api-keys" {
		t.Errorf("Name = %q, want stripe-api-keys", got)
	}
}
