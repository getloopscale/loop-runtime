// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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
)

// VaultRoleIDSyncSeed is the Go port of
// infra/scripts/local/seed-vault-role-id-sync.sh.
//
// Reads the fresh `loopzero-api` AppRole role_id from OpenBao (via port-forward
// to the openbao-0 pod in the vault cluster) and upserts it on the mgmt/loopzero-
// system/loopzero-api Deployment env `OPENBAO_VAULT_ROLE_ID`.
//
// The bash version execs `bao read` inside the pod; the Go port hits the
// OpenBao HTTP API `/v1/auth/approle/role/loopzero-api/role-id` directly. Same
// authoritative source — no shell-out to `bao`.
//
// Parity contract:
//   - unseal file missing / no root_token       → FAIL
//   - port-forward to vault fails               → FAIL
//   - HTTP read of role-id returns empty/non-200 → FAIL
//   - success                                    → PASS
type VaultRoleIDSyncSeed struct{}

func NewVaultRoleIDSyncSeed() *VaultRoleIDSyncSeed { return &VaultRoleIDSyncSeed{} }
func (s *VaultRoleIDSyncSeed) Name() string        { return "vault-role-id-sync" }

const (
	vaultUnsealFileEnv         = "LOOPZERO_VAULT_UNSEAL_FILE"
	vaultUnsealFileDefault     = ".loopzero/cleanroom-unseal.json"
	vaultOpenBaoPortRemote     = 8200
	openBaoVaultRoleIDEnvVar   = "OPENBAO_VAULT_ROLE_ID"
	vaultRoleIDReadPathLoopZeroAPI = "/v1/auth/approle/role/loopzero-api/role-id"
)

func (s *VaultRoleIDSyncSeed) Run(d Deps) Result {
	rootToken, err := readVaultUnsealRootToken()
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
		return Result{Verdict: PASS, Reason: fmt.Sprintf("dry-run: would upsert %s=%s… on loopzero-api", openBaoVaultRoleIDEnvVar, roleID[:8])}
	}
	// Idempotent env upsert on mgmt loopzero-api.
	if err := setDeploymentContainerEnv(d.Ctx, d.CtrlClient,
		loopzeroAPINamespaceInCore, loopzeroAPIDeploymentInCore, openBaoVaultRoleIDEnvVar, roleID); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("set env: %v", err)}
	}
	if err := waitForRolloutSettled(d.Ctx, d.CtrlClient,
		loopzeroAPINamespaceInCore, loopzeroAPIDeploymentInCore, d.settleTimeout(loopzeroAPISettle)); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("rollout: %v", err)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("canonical role_id synced (first 8=%s…)", roleID[:8])}
}

// Verify probes that the mgmt loopzero-api Deployment env carries a non-empty
// OPENBAO_VAULT_ROLE_ID. It does NOT re-open the vault tunnel — the deep
// check requires reading from vault which is a separate cost.
func (s *VaultRoleIDSyncSeed) Verify(d Deps) Result {
	got, err := getDeploymentContainerEnv(d.Ctx, d.CtrlClient,
		loopzeroAPINamespaceInCore, loopzeroAPIDeploymentInCore, openBaoVaultRoleIDEnvVar)
	if err != nil {
		return Result{Verdict: FAIL, Reason: err.Error()}
	}
	if got == "" || len(got) < 30 {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s = %q (want ≥30-char UUID)", openBaoVaultRoleIDEnvVar, got)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("%s wired (%d chars)", openBaoVaultRoleIDEnvVar, len(got))}
}

// readVaultUnsealRootToken pulls .root_token from the Cleanroom unseal file
// (canonical dev-only artifact per vault-bootstrap-dev.sh).
func readVaultUnsealRootToken() (string, error) {
	path := osGetenv(vaultUnsealFileEnv)
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
		base+vaultRoleIDReadPathLoopZeroAPI, nil)
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
