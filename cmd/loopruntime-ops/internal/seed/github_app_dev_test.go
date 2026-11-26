// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// fakeKey is a small alias so the tests read like the fetch calls in kube.go.
func fakeKey(ns, name string) client.ObjectKey { return client.ObjectKey{Namespace: ns, Name: name} }

// testPKCS1PEM is generated once per test binary run and cached. RSA-2048 keygen
// is expensive (~200ms) but running it once amortizes across all subtests.
var (
	testPKCS1Once sync.Once
	testPKCS1PEM  string
)

func loadTestPKCS1PEM(t *testing.T) string {
	t.Helper()
	testPKCS1Once.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate RSA: %v", err)
		}
		testPKCS1PEM = string(pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(key),
		}))
	})
	return testPKCS1PEM
}

func writeTestCreds(t *testing.T, pem, webhook string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "dev-credentials.json")
	content := `{"github":{"app_id":"3319955","app_private_key":` +
		mustJSONString(pem) + `,"webhook_secret":` + mustJSONString(webhook) + `}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write creds: %v", err)
	}
	return path
}

func mustJSONString(s string) string {
	// Minimal JSON string escape — the test PEM has no problematic runes.
	out := []byte{'"'}
	for _, r := range s {
		switch r {
		case '\\':
			out = append(out, '\\', '\\')
		case '"':
			out = append(out, '\\', '"')
		case '\n':
			out = append(out, '\\', 'n')
		default:
			out = append(out, byte(r))
		}
	}
	return string(append(out, '"'))
}

func TestGitHubAppDev_CreatesSecretsWhenAbsent(t *testing.T) {
	credsPath := writeTestCreds(t, loadTestPKCS1PEM(t), "canonical-webhook-secret")
	creds, err := LoadCreds(credsPath)
	if err != nil {
		t.Fatalf("LoadCreds: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: creds}
	seed := NewGitHubAppDevSeed()

	got := seed.Run(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verdict = %s (%s), want PASS", got.Verdict, got.Reason)
	}
	var s1 corev1.Secret
	if err := c.Get(context.Background(), fakeKey("loopruntime-system", "loopruntime-api-github"), &s1); err != nil {
		t.Fatalf("loopruntime-api-github not created: %v", err)
	}
	// Bash parity: trailing newlines stripped before Secret write.
	wantPEM := loadTestPKCS1PEM(t)
	for len(wantPEM) > 0 && wantPEM[len(wantPEM)-1] == '\n' {
		wantPEM = wantPEM[:len(wantPEM)-1]
	}
	if string(s1.Data["GITHUB_APP_PRIVATE_KEY"]) != wantPEM {
		t.Errorf("GITHUB_APP_PRIVATE_KEY mismatch")
	}
	var s2 corev1.Secret
	if err := c.Get(context.Background(), fakeKey("loopruntime-system", "loopruntime-github-webhook"), &s2); err != nil {
		t.Fatalf("loopruntime-github-webhook not created: %v", err)
	}
	if string(s2.Data["value"]) != "canonical-webhook-secret" {
		t.Errorf("webhook value = %q, want canonical-webhook-secret", s2.Data["value"])
	}
}

func TestGitHubAppDev_NoopWhenConverged(t *testing.T) {
	credsPath := writeTestCreds(t, loadTestPKCS1PEM(t), "canonical-webhook-secret")
	creds, _ := LoadCreds(credsPath)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: creds}
	seed := NewGitHubAppDevSeed()

	// Two consecutive runs; second must PASS just as first (idempotent).
	if r := seed.Run(deps); r.Verdict != PASS {
		t.Fatalf("first run: %s (%s), want PASS", r.Verdict, r.Reason)
	}
	if r := seed.Run(deps); r.Verdict != PASS {
		t.Fatalf("second run (converged): %s (%s), want PASS", r.Verdict, r.Reason)
	}
}

func TestGitHubAppDev_SkipWhenCredsAbsent(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: nil}
	seed := NewGitHubAppDevSeed()

	got := seed.Run(deps)

	if got.Verdict != SKIP {
		t.Fatalf("Verdict = %s (%s), want SKIP (nil Creds)", got.Verdict, got.Reason)
	}
	// Assert no Secret created (probe the negative).
	var s1 corev1.Secret
	if err := c.Get(context.Background(), fakeKey("loopruntime-system", "loopruntime-api-github"), &s1); err == nil {
		t.Error("loopruntime-api-github created despite SKIP (should be no-op)")
	}
}

func TestGitHubAppDev_SkipWhenAppIDPlaceholder(t *testing.T) {
	credsPath := writeTestCreds(t, loadTestPKCS1PEM(t), "canonical-webhook-secret")
	creds, _ := LoadCreds(credsPath)
	creds.GitHub.AppID = "" // simulate placeholder / missing
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: creds}

	got := NewGitHubAppDevSeed().Run(deps)

	if got.Verdict != SKIP {
		t.Fatalf("Verdict = %s (%s), want SKIP (github.app_id empty)", got.Verdict, got.Reason)
	}
}

func TestGitHubAppDev_FailWhenMalformedPEM(t *testing.T) {
	credsPath := writeTestCreds(t, "REDACTED_SECRET_005", "wh")
	creds, _ := LoadCreds(credsPath)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: creds}

	got := NewGitHubAppDevSeed().Run(deps)

	if got.Verdict != FAIL {
		t.Fatalf("Verdict = %s (%s), want FAIL (bad PEM)", got.Verdict, got.Reason)
	}
	// No Secret should have been created on FAIL (fail-fast at pre-condition).
	var s1 corev1.Secret
	if err := c.Get(context.Background(), fakeKey("loopruntime-system", "loopruntime-api-github"), &s1); err == nil {
		t.Error("loopruntime-api-github created despite FAIL (fail-fast violated)")
	}
}

func TestGitHubAppDev_VerifyPassesWhenSecretsMatch(t *testing.T) {
	credsPath := writeTestCreds(t, loadTestPKCS1PEM(t), "canonical-webhook-secret")
	creds, _ := LoadCreds(credsPath)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: creds}
	seed := NewGitHubAppDevSeed()

	seed.Run(deps) // set up the desired state
	got := seed.Verify(deps)

	if got.Verdict != PASS {
		t.Fatalf("Verify = %s (%s), want PASS after Run", got.Verdict, got.Reason)
	}
}

func TestGitHubAppDev_VerifyFailsWhenSecretMissing(t *testing.T) {
	credsPath := writeTestCreds(t, loadTestPKCS1PEM(t), "canonical-webhook-secret")
	creds, _ := LoadCreds(credsPath)
	c := fake.NewClientBuilder().WithScheme(newScheme(t)).Build()
	deps := Deps{Ctx: context.Background(), Kube: NewKubeClient(c), Creds: creds}

	// Do not Run; Verify must FAIL because the Secret is absent.
	got := NewGitHubAppDevSeed().Verify(deps)

	if got.Verdict != FAIL {
		t.Fatalf("Verify without Run = %s (%s), want FAIL", got.Verdict, got.Reason)
	}
}

func TestGitHubAppDev_Name(t *testing.T) {
	if got := NewGitHubAppDevSeed().Name(); got != "github-app-dev" {
		t.Errorf("Name = %q, want github-app-dev", got)
	}
}
