// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func writeStripeCreds(t *testing.T, webhookSecret string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "dev-credentials.json")
	content := `{"stripe":{"webhook_secret":` + mustJSONString(webhookSecret) + `}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write creds: %v", err)
	}
	return path
}

func TestStripeWebhookSecret_CreatesSecretWhenAbsent(t *testing.T) {
	credsPath := writeStripeCreds(t, "whsec_1qEhZO_real_test_secret")
	creds, err := LoadCreds(credsPath)
	if err != nil {
		t.Fatalf("LoadCreds: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: creds}

	got := NewStripeWebhookSecretSeed().Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	var s corev1.Secret
	if err := c.Get(context.Background(), fakeKey("loopruntime-system", "loopruntime-stripe-webhook"), &s); err != nil {
		t.Fatalf("Secret not created: %v", err)
	}
	if string(s.Data["STRIPE_WEBHOOK_SECRET"]) != "whsec_1qEhZO_real_test_secret" {
		t.Errorf("value mismatch, got %q", s.Data["STRIPE_WEBHOOK_SECRET"])
	}
}

func TestStripeWebhookSecret_NoopWhenConverged(t *testing.T) {
	credsPath := writeStripeCreds(t, "whsec_real")
	creds, _ := LoadCreds(credsPath)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: creds}
	seed := NewStripeWebhookSecretSeed()

	if r := seed.Run(deps); r.Verdict != PASS {
		t.Fatalf("first: %s (%s), want PASS", r.Verdict, r.Reason)
	}
	if r := seed.Run(deps); r.Verdict != PASS {
		t.Fatalf("second (converged): %s (%s), want PASS", r.Verdict, r.Reason)
	}
}

func TestStripeWebhookSecret_SkipWhenCredsAbsent(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: nil}

	got := NewStripeWebhookSecretSeed().Run(deps)

	if got.Verdict != SKIP {
		t.Fatalf("Verdict = %s (%s), want SKIP", got.Verdict, got.Reason)
	}
	// Assert no Secret created.
	var s corev1.Secret
	if err := c.Get(context.Background(), fakeKey("loopruntime-system", "loopruntime-stripe-webhook"), &s); err == nil {
		t.Error("Secret created despite SKIP")
	}
}

func TestStripeWebhookSecret_SkipWhenLiteralPlaceholder(t *testing.T) {
	credsPath := writeStripeCreds(t, "whsec_PLACEHOLDER")
	creds, _ := LoadCreds(credsPath)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: creds}

	got := NewStripeWebhookSecretSeed().Run(deps)

	if got.Verdict != SKIP {
		t.Fatalf("Verdict = %s (%s), want SKIP (literal placeholder)", got.Verdict, got.Reason)
	}
}

func TestStripeWebhookSecret_SkipWhenGenericPlaceholder(t *testing.T) {
	credsPath := writeStripeCreds(t, "whsec_dev_abc") // matches IsPlaceholder ("whsec_dev_")
	creds, _ := LoadCreds(credsPath)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: creds}

	got := NewStripeWebhookSecretSeed().Run(deps)

	if got.Verdict != SKIP {
		t.Fatalf("Verdict = %s (%s), want SKIP (whsec_dev_ placeholder)", got.Verdict, got.Reason)
	}
}

func TestStripeWebhookSecret_SkipWhenEmpty(t *testing.T) {
	credsPath := writeStripeCreds(t, "")
	creds, _ := LoadCreds(credsPath)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: creds}

	got := NewStripeWebhookSecretSeed().Run(deps)

	if got.Verdict != SKIP {
		t.Fatalf("Verdict = %s (%s), want SKIP (empty)", got.Verdict, got.Reason)
	}
}

func TestStripeWebhookSecret_VerifyPassesAfterRun(t *testing.T) {
	credsPath := writeStripeCreds(t, "whsec_real")
	creds, _ := LoadCreds(credsPath)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: creds}
	seed := NewStripeWebhookSecretSeed()

	seed.Run(deps)
	got := seed.Verify(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verify = %s (%s), want PASS", got.Verdict, got.Reason)
	}
}

func TestStripeWebhookSecret_VerifyFailsWhenAbsent(t *testing.T) {
	credsPath := writeStripeCreds(t, "whsec_real")
	creds, _ := LoadCreds(credsPath)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: creds}

	got := NewStripeWebhookSecretSeed().Verify(deps)

	if got.Verdict != FAIL {
		t.Fatalf("Verify without Run = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
}

func TestStripeWebhookSecret_Name(t *testing.T) {
	if got := NewStripeWebhookSecretSeed().Name(); got != "stripe-webhook-secret" {
		t.Errorf("Name = %q, want stripe-webhook-secret", got)
	}
}
