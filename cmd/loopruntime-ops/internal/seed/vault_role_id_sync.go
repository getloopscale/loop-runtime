// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// VaultRoleIDSyncSeed is the Go port of
// infra/scripts/local/seed-vault-role-id-sync.sh.
//
// Reads the fresh `loopruntime-api` AppRole role_id from OpenBao (via port-forward
// to the openbao-0 pod in the vault cluster) and writes it as
// `OPENBAO_VAULT_ROLE_ID` into Secret loopruntime-system/loopruntime-openbao-approle — the Secret
// vault-bootstrap-dev.sh created with the secret_id, and which the loopruntime-api chart
// already reads through envFrom. Then it restarts loopruntime-api so the pod reads it.
//
// It used to upsert the value as an env var on the loopruntime-api Deployment spec. That
// spec is rendered by helmfile, an env var overrides every envFrom source, and the
// override outlived the next sync — so the stale role id committed in the values
// was never noticed (principle_violations#67). The role id changes at every vault
// bootstrap, which is what makes it seed material: it cannot be a value in a file.
//
// The bash version execs `bao read` inside the pod; the Go port hits the
// OpenBao HTTP API `/v1/auth/approle/role/loopruntime-api/role-id` directly. Same
// authoritative source — no shell-out to `bao`.
//
// Parity contract:
//   - unseal file missing / no root_token          → FAIL
//   - port-forward to vault fails                  → FAIL
//   - HTTP read of role-id returns empty/non-200   → FAIL
//   - Secret loopruntime-openbao-approle absent           → FAIL (vault-bootstrap-dev.sh has not run)
//   - success                                      → PASS
type VaultRoleIDSyncSeed struct{}

func NewVaultRoleIDSyncSeed() *VaultRoleIDSyncSeed { return &VaultRoleIDSyncSeed{} }
func (s *VaultRoleIDSyncSeed) Name() string        { return "vault-role-id-sync" }

const (
	vaultContextName                  = "k3d-loopruntime-secrets"
	vaultNamespace                    = "secrets-system"
	vaultUnsealFileEnv                = "LOOPRUNTIME_VAULT_UNSEAL_FILE"
	vaultUnsealFileDefault            = ".loopruntime/cleanroom-unseal.json"
	vaultOpenBaoPortRemote            = 8200
	openBaoVaultRoleIDKey             = "OPENBAO_VAULT_ROLE_ID"
	vaultRoleIDReadPathLoopRuntimeAPI = "/v1/auth/approle/role/loopruntime-api/role-id"
	// The Secret both AppRole halves live in, where loopruntime-api reads them (charts/loopruntime-api
	// values-bootstrap.yaml secrets.additionalSources; infra/tilt/api-deployment.yaml).
	openBaoAppRoleNamespace        = "loopruntime-system"
	openBaoAppRoleSecret           = "loopruntime-openbao-approle"
	loopruntimeAPINamespaceInCore  = "loopruntime-system"
	loopruntimeAPIDeploymentInCore = "loopruntime-api"
)

func (s *VaultRoleIDSyncSeed) Run(d Deps) Result {
	rootToken, err := readVaultUnsealRootToken(d.Host)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("unseal file: %v", err)}
	}
	// Port-forward openbao Service in the vault cluster.
	if d.PortForwardCtor == nil {
		return Result{Verdict: FAIL, Reason: "PortForwardCtor not wired"}
	}
	tunnel, err := d.PortForwardCtor(d.Ctx, TunnelTarget{
		Context: vaultContextName, Namespace: vaultNamespace, PodLabels: PodLabels{"app.kubernetes.io/name": "openbao"},
		RemotePort: vaultOpenBaoPortRemote, ProbePath: "",
	})
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("port-forward openbao (vault ctx): %v", err)}
	}
	defer tunnel.Close()

	base := fmt.Sprintf("https://127.0.0.1:%d", tunnel.LocalPort)
	roleID, err := openBaoReadRoleID(d.Ctx, base, rootToken)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("read role-id: %v", err)}
	}
	if len(roleID) < 30 {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("role_id too short (%d chars)", len(roleID))}
	}

	if d.DryRun {
		return Result{Verdict: PASS, Reason: fmt.Sprintf("dry-run: would write %s=%s… into %s/%s and restart loopruntime-api",
			openBaoVaultRoleIDKey, roleID[:8], openBaoAppRoleNamespace, openBaoAppRoleSecret)}
	}
	// The Secret must already exist: vault-bootstrap-dev.sh creates it with the
	// secret_id, and a role id with no secret_id to pair with authenticates nothing.
	// PatchSecretData keeps the keys it does not name.
	if err := d.Kube.PatchSecretData(d.Ctx, openBaoAppRoleNamespace, openBaoAppRoleSecret,
		map[string][]byte{openBaoVaultRoleIDKey: []byte(roleID)}); err != nil {
		if apierrors.IsNotFound(err) {
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("Secret %s/%s does not exist — run vault-bootstrap-dev.sh first, it writes the secret_id this role id pairs with",
				openBaoAppRoleNamespace, openBaoAppRoleSecret)}
		}
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("write %s into %s/%s: %v", openBaoVaultRoleIDKey, openBaoAppRoleNamespace, openBaoAppRoleSecret, err)}
	}
	// envFrom is read at container start: restart so the pod reads the new value.
	// Setup runs this before the dev loop deploys loopruntime-api (#405). With no Deployment
	// nothing holds a stale value, and the first container start reads the Secret.
	if err := d.Kube.RolloutRestartDeployment(d.Ctx, loopruntimeAPINamespaceInCore, loopruntimeAPIDeploymentInCore); err != nil {
		if apierrors.IsNotFound(err) {
			return Result{Verdict: PASS, Reason: fmt.Sprintf("canonical role_id written to %s/%s (first 8=%s…); loopruntime-api not deployed yet, its first start reads it",
				openBaoAppRoleNamespace, openBaoAppRoleSecret, roleID[:8])}
		}
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("restart loopruntime-api: %v", err)}
	}
	// The restarted pods existing is the delivery; their readiness is not this seed's to
	// wait for — on dev-public loopruntime-api has no database yet at this point in setup (#444).
	if err := waitForRolloutApplied(d.Ctx, d.CtrlClient,
		loopruntimeAPINamespaceInCore, loopruntimeAPIDeploymentInCore, d.settleTimeout(loopruntimeAPISettle)); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("rollout: %v", err)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("canonical role_id written to %s/%s (first 8=%s…)", openBaoAppRoleNamespace, openBaoAppRoleSecret, roleID[:8])}
}

// Verify probes that Secret loopruntime-openbao-approle carries a role id of the shape
// OpenBao issues. It does NOT re-open the vault tunnel — the deep check requires
// reading from vault, which is a separate cost.
func (s *VaultRoleIDSyncSeed) Verify(d Deps) Result {
	sec, err := d.Kube.GetSecret(d.Ctx, openBaoAppRoleNamespace, openBaoAppRoleSecret)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get %s/%s: %v", openBaoAppRoleNamespace, openBaoAppRoleSecret, err)}
	}
	got := string(sec.Data[openBaoVaultRoleIDKey])
	if len(got) < 30 {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s/%s %s is %d chars (want a ≥30-char role id)",
			openBaoAppRoleNamespace, openBaoAppRoleSecret, openBaoVaultRoleIDKey, len(got))}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("%s wired in %s/%s (%d chars)", openBaoVaultRoleIDKey, openBaoAppRoleNamespace, openBaoAppRoleSecret, len(got))}
}

// readVaultUnsealRootToken pulls .root_token from the Cleanroom unseal file
// (canonical dev-only artifact per vault-bootstrap-dev.sh).
func readVaultUnsealRootToken(h Host) (string, error) {
	path := h.getenv(vaultUnsealFileEnv)
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, vaultUnsealFileDefault)
	}
	b, err := os.ReadFile(path) // #nosec G304 -- operator-local unseal file
	if err != nil {
		return "", fmt.Errorf("read %s: %w (run vault-bootstrap-dev.sh first)", path, err)
	}
	var d struct {
		RootToken string `json:"root_token"`
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}
	if d.RootToken == "" {
		return "", fmt.Errorf("root_token empty in %s", path)
	}
	return d.RootToken, nil
}

// openBaoReadRoleID hits the AppRole read endpoint with the root token.
// Response shape: {"data":{"role_id":"<uuid>"}}.
func openBaoReadRoleID(ctx context.Context, base, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		base+vaultRoleIDReadPathLoopRuntimeAPI, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Vault-Token", token)
	req.Header.Set("X-Vault-Request", "true")
	// OpenBao ships with a self-signed cert in dev; canonical to skip verify.
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // #nosec G402 -- dev-only cleanroom TLS
	resp, err := (&http.Client{Timeout: 10 * time.Second, Transport: tr}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(b))
	}
	var out struct {
		Data struct {
			RoleID string `json:"role_id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Data.RoleID == "" {
		return "", fmt.Errorf("empty role_id in response")
	}
	return out.Data.RoleID, nil
}
