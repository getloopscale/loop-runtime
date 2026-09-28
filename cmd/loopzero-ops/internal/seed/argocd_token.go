// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ArgoCDTokenSeed is the Go port of
// infra/scripts/local/seed-argocd-token-canonical.sh.
//
// It mints an ArgoCD account token (account=loopzero-api, name=canonical-seed)
// via the admin session and syncs it into loopzero-system/loopzero-api-tokens under
// the `argocd-token` key. Post-apply: rollout restart loopzero-api.
//
// Parity contract (D3):
//   - argocd/argocd-initial-admin-secret missing / password empty → FAIL
//   - port-forward failure                                        → FAIL
//   - session login returns empty token                           → FAIL
//   - account token mint returns empty                            → FAIL
//   - k8s API error / rollout error                               → FAIL
//   - success                                                     → PASS
type ArgoCDTokenSeed struct{}

func NewArgoCDTokenSeed() *ArgoCDTokenSeed { return &ArgoCDTokenSeed{} }
func (s *ArgoCDTokenSeed) Name() string    { return "argocd-token" }

const (
	argocdInitialAdminSecret = "argocd-initial-admin-secret"
	argocdInitialAdminKey    = "password"
	loopzeroAPITokensSecret      = "loopzero-api-tokens"
	loopzeroAPITokensKey         = "argocd-token"
)

func (s *ArgoCDTokenSeed) Run(d Deps) Result {
	pwSec, err := d.Kube.GetSecret(d.Ctx, argocdNamespace, argocdInitialAdminSecret)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get %s/%s: %v (run task setup first)", argocdNamespace, argocdInitialAdminSecret, err)}
	}
	pw := string(pwSec.Data[argocdInitialAdminKey])
	if pw == "" {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s/%s.%s empty", argocdNamespace, argocdInitialAdminSecret, argocdInitialAdminKey)}
	}
	if d.DryRun {
		return Result{Verdict: PASS, Reason: "dry-run: would mint argocd token + sync to loopzero-api-tokens + rollout loopzero-api"}
	}
	if d.PortForwardCtor == nil {
		return Result{Verdict: FAIL, Reason: "PortForwardCtor not wired (composition root gap)"}
	}
	// Port-forward argocd-server (svc port 80 → target 8080 plaintext, canonical).
	tunnel, err := d.PortForwardCtor(d.Ctx, TunnelTarget{
		Namespace: argocdNamespace, PodLabels: PodMetaLabels("argocd-server"),
		RemotePort: 8080, ProbePath: "/healthz",
	})
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("port-forward argocd-server: %v", err)}
	}
	defer tunnel.Close()

	base := fmt.Sprintf("http://127.0.0.1:%d", tunnel.LocalPort)

	// Step 1: session login → session token
	session, err := argocdSessionLogin(d.Ctx, base, "admin", pw)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("session login: %v", err)}
	}
	// Step 2: account token mint
	token, err := argocdMintAccountToken(d.Ctx, base, session, "loopzero-api", "canonical-seed")
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("mint account token: %v", err)}
	}
	// Step 3: sync to loopzero-api-tokens
	// B-108 — this seed runs on its own (`loopzero-ops seed argocd-token`), which is how an operator
	// repairs one piece without repeating the whole chain. Nothing then guarantees
	// the namespace, and the failure is silent on our side: the seed stops, the
	// chain goes on, and the effect shows up in another system. Idempotent — a
	// Get when the namespace exists, which is the usual path.
	if err := d.Kube.EnsureNamespace(d.Ctx, minioNamespace); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("ensure namespace %s: %v", minioNamespace, err)}
	}
	// loopzero-api-tokens is shared (gitea-token from the gitea-token seed,
	// cf-api-token from External Secrets): write only the argocd-token key.
	if err := d.Kube.MergeSecretData(d.Ctx, minioNamespace, loopzeroAPITokensSecret,
		map[string][]byte{loopzeroAPITokensKey: []byte(token)}); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("apply %s/%s: %v", minioNamespace, loopzeroAPITokensSecret, err)}
	}
	// Step 4: rollout restart loopzero-api. A loopzero-api not deployed yet reads the
	// token when it starts, so there is nothing to restart — the same rule the
	// gitea-token seed applies. Any other error is a real failure.
	if err := d.Kube.RolloutRestartDeployment(d.Ctx, minioNamespace, minioLoopZeroAPIDeployment); err != nil {
		if !isNotFoundErr(err) {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("rollout %s/%s: %v", minioNamespace, minioLoopZeroAPIDeployment, err)}
		}
		return Result{Verdict: PASS, Reason: fmt.Sprintf("minted argocd token → %s/%s (%s not deployed yet — nothing to restart)", minioNamespace, loopzeroAPITokensSecret, minioLoopZeroAPIDeployment)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("minted argocd token → %s/%s + rollout %s", minioNamespace, loopzeroAPITokensSecret, minioLoopZeroAPIDeployment)}
}

// Verify probes the Secret exists AND the argocd-token key is non-empty.
// It does NOT attempt to authenticate against ArgoCD (that would require a
// second port-forward — the empty-value assertion is a strong enough drift
// probe for the operator use case).
func (s *ArgoCDTokenSeed) Verify(d Deps) Result {
	got, err := d.Kube.GetSecret(d.Ctx, minioNamespace, loopzeroAPITokensSecret)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get %s/%s: %v", minioNamespace, loopzeroAPITokensSecret, err)}
	}
	if len(got.Data[loopzeroAPITokensKey]) == 0 {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s/%s.%s missing/empty", minioNamespace, loopzeroAPITokensSecret, loopzeroAPITokensKey)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("%s/%s.%s present", minioNamespace, loopzeroAPITokensSecret, loopzeroAPITokensKey)}
}

// argocdSessionLogin POSTs /api/v1/session with admin credentials and returns
// the session bearer token from the response body.
func argocdSessionLogin(ctx context.Context, base, user, pw string) (string, error) {
	body, _ := json.Marshal(map[string]string{"username": user, "password": pw})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/session", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(b))
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Token == "" {
		return "", fmt.Errorf("empty session token in response")
	}
	return out.Token, nil
}

// argocdMintAccountToken POSTs /api/v1/account/<acct>/token with the session
// bearer + a token name. Returns the newly-minted token.
func argocdMintAccountToken(ctx context.Context, base, session, account, name string) (string, error) {
	body, _ := json.Marshal(map[string]string{"name": name})
	url := fmt.Sprintf("%s/api/v1/account/%s/token", base, account)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+session)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(b))
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Token == "" {
		return "", fmt.Errorf("empty account token in response")
	}
	return out.Token, nil
}

func snippet(b []byte) string {
	const max = 200
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}
