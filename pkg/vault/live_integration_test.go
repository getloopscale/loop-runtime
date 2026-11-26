// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package vault

import (
	"context"
	"crypto/tls"
	"net/http"
	"os"
	"testing"
	"time"

	bao "github.com/openbao/openbao/api/v2"
)

// liveVaultPrefix is the canonical OpenBao/Vault Transit ciphertext prefix.
// Duplicated here (string) for test isolation; canonical bytes form lives in
// api/internal/db/secret_wrapper.go (cross-package coupling avoided).
const liveVaultPrefix = "vault:v1:"

// insecureTLS returns a TLS config that skips cert verification — used for
// LIVE tests against k3d self-signed Vault. PRODUCTION code MUST never use
// this (TransitClient pins the server SPKI instead).
func insecureTLS() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true} // #nosec G402 — test-only
}

// liveAddr returns the live Vault address from the env or skips the test.
// Tests guarded by this helper run ONLY when VAULT_ADDR + VAULT_TOKEN are set
// (e.g., LIVE k3d cluster + manual orchestration). Default skip preserves CI
// determinism.
func liveAddr(t *testing.T) (string, string) {
	t.Helper()
	addr := os.Getenv("VAULT_ADDR")
	token := os.Getenv("VAULT_TOKEN")
	if addr == "" || token == "" {
		t.Skip("LIVE skipped: VAULT_ADDR + VAULT_TOKEN required")
	}
	return addr, token
}

// liveClient constructs an SDK client honoring VAULT_SKIP_VERIFY=true for k3d
// self-signed TLS scenarios. Production Vault is reached over HTTPS with a CA
// bundle.
func liveClient(t *testing.T, addr, token string) *bao.Client {
	t.Helper()
	cfg := bao.DefaultConfig()
	cfg.Address = addr
	if os.Getenv("VAULT_SKIP_VERIFY") == "true" {
		cfg.HttpClient = &http.Client{Transport: &http.Transport{
			TLSClientConfig: insecureTLS(),
		}}
	}
	c, err := bao.NewClient(cfg)
	if err != nil {
		t.Fatalf("bao.NewClient: %v", err)
	}
	c.SetToken(token)
	return c
}

// TestLIVE_AuthMethods_Provisioned verifies T5.1-T5.4 — all 6 K8s auth mounts
// exist in the live Vault. Runs only with VAULT_ADDR + VAULT_TOKEN set.
func TestLIVE_AuthMethods_Provisioned(t *testing.T) {
	addr, token := liveAddr(t)
	c := liveClient(t, addr, token)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	auths, err := c.Sys().ListAuthWithContext(ctx)
	if err != nil {
		t.Fatalf("ListAuth: %v", err)
	}
	expected := []string{
		"kubernetes/",
		"k8s-core/",
		"k8s-build/",
		"k8s-cell-0/",
		"k8s-cell-0/",
		"k8s-cell-1/",
	}
	for _, want := range expected {
		if _, ok := auths[want]; !ok {
			t.Errorf("LIVE FAIL: auth mount %q absent (T5.1-T5.4 not provisioned)", want)
		}
	}
}

// TestLIVE_SecretsEngines_Provisioned verifies T6.1 + T7.1 — database +
// kubernetes engines mounted alongside Transit + KV-v2.
func TestLIVE_SecretsEngines_Provisioned(t *testing.T) {
	addr, token := liveAddr(t)
	c := liveClient(t, addr, token)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mounts, err := c.Sys().ListMountsWithContext(ctx)
	if err != nil {
		t.Fatalf("ListMounts: %v", err)
	}
	expected := []string{"secret/", "transit/", "database/", "kubernetes/"}
	for _, want := range expected {
		if _, ok := mounts[want]; !ok {
			t.Errorf("LIVE FAIL: engine %q absent", want)
		}
	}
}

// TestLIVE_TransitEncryptDecrypt_RoundTrip verifies T3.4 LIVE — bootstrap key
// produces vault:v1: prefixed ciphertext that decrypts back to plaintext.
func TestLIVE_TransitEncryptDecrypt_RoundTrip(t *testing.T) {
	addr, token := liveAddr(t)
	c := liveClient(t, addr, token)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const plaintext = "live-test-2026-06-03"
	encrypted, err := c.Logical().WriteWithContext(ctx, "transit/encrypt/bootstrap", map[string]interface{}{
		"plaintext": "bGl2ZS10ZXN0LTIwMjYtMDYtMDM=", // base64("live-test-2026-06-03")
	})
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	ciphertext, _ := encrypted.Data["ciphertext"].(string)
	if len(ciphertext) < len(liveVaultPrefix) || ciphertext[:len(liveVaultPrefix)] != liveVaultPrefix {
		t.Errorf("LIVE FAIL: ciphertext missing vault:v1: prefix, got %q", ciphertext)
	}

	decrypted, err := c.Logical().WriteWithContext(ctx, "transit/decrypt/bootstrap", map[string]interface{}{
		"ciphertext": ciphertext,
	})
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	out, _ := decrypted.Data["plaintext"].(string)
	if out != "bGl2ZS10ZXN0LTIwMjYtMDYtMDM=" {
		t.Errorf("LIVE FAIL: round-trip mismatch, decrypted=%q", out)
		_ = plaintext
	}
}

// TestLIVE_PolicyDenial_403 verifies T8.6 — a low-priv token writing outside
// policy scope returns 403 + permission denied (audit log_request_failure
// counter increments, satisfying VaultPolicyDenialRateHigh alert query).
func TestLIVE_PolicyDenial_403(t *testing.T) {
	addr, token := liveAddr(t)
	root := liveClient(t, addr, token)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create a child token bound to eso-read policy (read-only on loop-runtime paths).
	secret, err := root.Auth().Token().CreateWithContext(ctx, &bao.TokenCreateRequest{
		Policies: []string{"eso-read"},
		TTL:      "60s",
	})
	if err != nil {
		t.Fatalf("create child token: %v", err)
	}
	childToken := secret.Auth.ClientToken

	denied := liveClient(t, addr, childToken)
	denied.SetToken(childToken)
	// Attempt write to a path NOT covered by eso-read.
	_, err = denied.Logical().WriteWithContext(ctx, "secret/data/loop-runtime/vault-local/should-fail-T8", map[string]interface{}{
		"data": map[string]interface{}{"value": "denied"},
	})
	if err == nil {
		t.Fatal("LIVE FAIL: expected permission denied error, got nil")
	}
	// Vault SDK wraps the 403; assert the error mentions denial.
	if !contains(err.Error(), "permission denied") && !contains(err.Error(), "403") {
		t.Errorf("LIVE FAIL: expected '403' or 'permission denied' in error, got %q", err.Error())
	}
}

// contains is a simple substring helper.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
