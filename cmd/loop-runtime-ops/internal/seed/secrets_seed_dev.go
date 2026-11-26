// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SecretsSeedDevSeed is the Go port of
// infra/scripts/local/secrets-seed-dev.sh.
//
// Writes 5 canonical OpenBao kv-v2 paths from ~/.loop-runtime/dev-credentials.json:
//
//	kv/loop-runtime/dev/api/secrets/github          (app_id + app_private_key + webhook_secret)
//	kv/loop-runtime/dev/cloud/secrets/stripe        (api_key + webhook_secret)
//	kv/loop-runtime/dev/cloud/secrets/stripe-webhook (whsec)
//	kv/loop-runtime/dev/cloud/secrets/resend        (api_key)
//	kv/loop-runtime/dev/platform/config/openbao     (vault_role_id)
//	kv/loop-runtime/dev/cloud/secrets/service-account (key)
//
// Missing sub-fields → skip that path (bash parity: `if [[ -n "$…" ]]`).
// Uses HTTP direct to the OpenBao kv-v2 write endpoint instead of `bao kv put`
// shell-out. Same authoritative source.
type SecretsSeedDevSeed struct{}

func NewSecretsSeedDevSeed() *SecretsSeedDevSeed { return &SecretsSeedDevSeed{} }
func (s *SecretsSeedDevSeed) Name() string       { return "secrets-seed-dev" }

func (s *SecretsSeedDevSeed) Run(d Deps) Result {
	if d.Creds == nil {
		return Result{Verdict: SKIP, Reason: "dev-credentials.json absent (SKIP-graceful)"}
	}
	rootToken, err := readVaultUnsealRootToken(d.Host)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("unseal: %v", err)}
	}
	if d.PortForwardCtor == nil {
		return Result{Verdict: FAIL, Reason: "PortForwardCtor not wired"}
	}
	tunnel, err := d.PortForwardCtor(d.Ctx, TunnelTarget{
		Context: vaultContextName, Namespace: vaultNamespace, PodLabels: PodLabels{"app.kubernetes.io/name": "openbao"},
		RemotePort: vaultOpenBaoPortRemote, ProbePath: "",
	})
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("port-forward openbao: %v", err)}
	}
	defer tunnel.Close()

	base := fmt.Sprintf("https://127.0.0.1:%d", tunnel.LocalPort)

	writes := []struct {
		path string
		data map[string]string
		when bool
	}{
		{
			path: "secret/data/loop-runtime/dev/api/secrets/github",
			data: map[string]string{
				"app_id":          d.Creds.GitHub.AppID,
				"app_private_key": d.Creds.GitHub.AppPrivateKey,
				"webhook_secret":  d.Creds.GitHub.WebhookSecret,
			},
			when: d.Creds.GitHub.AppID != "",
		},
		{
			path: "secret/data/loop-runtime/dev/cloud/secrets/stripe",
			data: map[string]string{
				"api_key":        d.Creds.Stripe.APIKey,
				"webhook_secret": d.Creds.Stripe.WebhookSecret,
			},
			when: d.Creds.Stripe.APIKey != "",
		},
		{
			path: "secret/data/loop-runtime/dev/cloud/secrets/stripe-webhook",
			data: map[string]string{"whsec": d.Creds.Stripe.WebhookSecret},
			when: d.Creds.Stripe.WebhookSecret != "",
		},
		{
			path: "secret/data/loop-runtime/dev/cloud/secrets/resend",
			data: map[string]string{"api_key": d.Creds.Resend.APIKey},
			when: d.Creds.Resend.APIKey != "",
		},
		{
			path: "secret/data/loop-runtime/dev/platform/config/openbao",
			data: map[string]string{"vault_role_id": d.Creds.OpenBao.VaultRoleID},
			when: d.Creds.OpenBao.VaultRoleID != "",
		},
		{
			path: "secret/data/loop-runtime/dev/cloud/secrets/service-account",
			data: map[string]string{"key": d.Creds.Cloud.ServiceAccountKey},
			when: d.Creds.Cloud.ServiceAccountKey != "",
		},
	}

	if d.DryRun {
		count := 0
		for _, w := range writes {
			if w.when {
				count++
			}
		}
		return Result{Verdict: PASS, Reason: fmt.Sprintf("dry-run: would write %d kv-v2 paths", count)}
	}

	writtenPaths := 0
	var writeErrs []error
	for _, w := range writes {
		if !w.when {
			continue
		}
		if err := openBaoKVPut(d.Ctx, base, rootToken, w.path, w.data); err != nil {
			writeErrs = append(writeErrs, fmt.Errorf("%s: %w", w.path, err))
			continue
		}
		writtenPaths++
	}
	if writtenPaths == 0 && len(writeErrs) == 0 {
		return Result{Verdict: SKIP, Reason: "no canonical creds populated in dev-credentials.json"}
	}
	// FIX-1 (M24, ADR-M24-1): fail-loud on ANY write failure. The previous
	// `failedPaths>0 → PASS` branch masked partial failure — an operator gating on
	// Verdict saw green while security secrets (stripe/resend/…) silently did not
	// land. errors.Join surfaces every failed path; recovery is by idempotent
	// re-run (no transaction — the writes are independent OpenBao HTTP calls, not
	// SQL; blueprint ADR-M24-2). Per .claude/rules/error-handling.md (fail-loud).
	if len(writeErrs) > 0 {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("wrote %d, %d failed: %v", writtenPaths, len(writeErrs), errors.Join(writeErrs...))}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("wrote %d canonical kv-v2 paths", writtenPaths)}
}

// Verify probes that the 5 canonical paths return non-404. Deeper content
// comparison is intentionally skipped — kv-v2 versions each write, so a
// deep probe would false-positive on legitimate operator manual writes.
func (s *SecretsSeedDevSeed) Verify(d Deps) Result {
	if d.Creds == nil {
		return Result{Verdict: SKIP, Reason: "dev-credentials.json absent"}
	}
	rootToken, err := readVaultUnsealRootToken(d.Host)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	if d.PortForwardCtor == nil {
		return Result{Verdict: FAIL, Reason: "PortForwardCtor not wired"}
	}
	tunnel, err := d.PortForwardCtor(d.Ctx, TunnelTarget{
		Context: vaultContextName, Namespace: vaultNamespace, PodLabels: PodLabels{"app.kubernetes.io/name": "openbao"},
		RemotePort: vaultOpenBaoPortRemote, ProbePath: "",
	})
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	defer tunnel.Close()

	base := fmt.Sprintf("https://127.0.0.1:%d", tunnel.LocalPort)
	// Just probe github path — enough to confirm the seed ran at some point.
	// A deeper matrix probe is future work.
	if ok, err := openBaoKVExists(d.Ctx, base, rootToken, "secret/data/loop-runtime/dev/api/secrets/github"); err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	} else if !ok {
		return Result{Verdict: FAIL, Reason: "kv/loop-runtime/dev/api/secrets/github missing"}
	}
	return Result{Verdict: PASS, Reason: "canonical github kv path present"}
}

// openBaoKVPut writes a kv-v2 secret at path with body `{"data": {...}}`.
func openBaoKVPut(ctx context.Context, base, token, path string, data map[string]string) error {
	body := map[string]interface{}{"data": data}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/"+path, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Vault-Token", token)
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // #nosec G402 -- dev-only cleanroom TLS
	resp, err := (&http.Client{Timeout: 10 * time.Second, Transport: tr}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(b))
	}
	return nil
}

// openBaoKVExists probes GET /v1/<path>. Returns (true, nil) on 200.
func openBaoKVExists(ctx context.Context, base, token, path string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/"+path, nil)
	if err != nil {
		return false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Vault-Token", token)
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // #nosec G402 -- dev-only
	resp, err := (&http.Client{Timeout: 10 * time.Second, Transport: tr}).Do(req)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}
