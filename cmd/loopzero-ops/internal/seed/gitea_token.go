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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// GiteaTokenSeed is the Go port of
// infra/scripts/local/seed-gitea-token-canonical.sh.
//
// 4-layer chain:
//
//  1. Authenticate to Gitea using gitea-admin.username + .password
//     1b. Ensure Gitea org `loopzero` exists (GET /orgs/loopzero → 200 no-op; 404 → POST /orgs)
//  2. Delete legacy token(s) named `loopzero-api`/`loopzero-api-token` + mint fresh one
//  3. Sync token to 3 consumer Secrets: gitea-admin.token, loopzero-api-tokens.gitea-token,
//     loopzero-tenant-operator.GITEA_TOKEN
//  4. Rollout restart loopzero-api + loopzero-tenant-operator (skip-with-INFO if absent)
type GiteaTokenSeed struct{}

func NewGiteaTokenSeed() *GiteaTokenSeed { return &GiteaTokenSeed{} }
func (s *GiteaTokenSeed) Name() string   { return "gitea-token" }

const (
	giteaTokenName               = "loopzero-api-token"
	loopzeroTenantOperatorSecret = "loopzero-tenant-operator" // #nosec G101 -- K8s Secret resource NAME, not a credential value
	loopzeroTenantOperatorKey    = "GITEA_TOKEN"
	loopzeroAPITokensGiteaKey    = "gitea-token"
	giteaAdminTokenSecretKey     = "token"
	loopzeroTenantOperatorDeploy = "loopzero-tenant-operator"
)

func (s *GiteaTokenSeed) Run(d Deps) Result {
	adm, err := d.Kube.GetSecret(d.Ctx, giteaAdminNamespace, giteaAdminSecretName)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 1: get %s/%s: %v (run task setup first)", giteaAdminNamespace, giteaAdminSecretName, err)}
	}
	user := string(adm.Data["username"])
	pw := string(adm.Data["password"])
	if user == "" {
		return Result{Verdict: FAIL, Reason: "Layer 1: gitea-admin.username missing — run task setup first"}
	}
	if pw == "" {
		return Result{Verdict: FAIL, Reason: "Layer 1: gitea-admin.password missing"}
	}
	if d.DryRun {
		return Result{Verdict: PASS, Reason: "dry-run: would mint fresh Gitea token + sync to 3 Secrets + restart 2 deployments"}
	}
	if d.PortForwardCtor == nil {
		return Result{Verdict: FAIL, Reason: "PortForwardCtor not wired"}
	}
	tunnel, err := d.PortForwardCtor(d.Ctx, TunnelTarget{
		Namespace: giteaAdminNamespace, PodLabels: PodMetaLabels("gitea"),
		RemotePort: 3000, ProbePath: "/api/v1/version",
	})
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("port-forward gitea: %v", err)}
	}
	defer tunnel.Close()

	base := fmt.Sprintf("http://127.0.0.1:%d", tunnel.LocalPort)

	// Layer 1 probe: HTTP 200 on /api/v1/user with basic auth.
	if err := giteaProbeAuth(d.Ctx, base, user, pw); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 1: credentials probe: %v", err)}
	}

	// Layer 1b: ensure org exists.
	if err := giteaEnsureOrg(d.Ctx, base, user, pw, "loopzero"); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 1b: %v", err)}
	}

	// Layer 2: delete legacy tokens (name==loopzero-api or loopzero-api-token) + mint fresh.
	if err := giteaDeleteLegacyTokens(d.Ctx, base, user, pw); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 2 delete: %v", err)}
	}
	token, err := giteaMintToken(d.Ctx, base, user, pw, giteaTokenName, []string{"all"})
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 2 mint: %v", err)}
	}

	// Layer 3: sync 3 consumer Secrets.
	tokenBytes := []byte(token)
	// B-108 — this seed runs on its own (`loopzero-ops seed gitea-token`), which is how an operator
	// repairs one piece without repeating the whole chain. Nothing then guarantees
	// the namespace, and the failure is silent on our side: the seed stops, the
	// chain goes on, and the effect shows up in another system. Idempotent — a
	// Get when the namespace exists, which is the usual path.
	if err := d.Kube.EnsureNamespace(d.Ctx, giteaAdminNamespace); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("ensure namespace %s: %v", giteaAdminNamespace, err)}
	}
	if err := d.Kube.PatchSecretData(d.Ctx, giteaAdminNamespace, giteaAdminSecretName,
		map[string][]byte{giteaAdminTokenSecretKey: tokenBytes}); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 3a: %v", err)}
	}
	// loopzero-api-tokens is shared (argocd-token from the argocd-token seed,
	// cf-api-token from External Secrets): write only the gitea-token key.
	if err := d.Kube.MergeSecretData(d.Ctx, giteaAdminNamespace, loopzeroAPITokensSecret,
		map[string][]byte{loopzeroAPITokensGiteaKey: tokenBytes}); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 3b: %v", err)}
	}
	if err := d.Kube.MergeSecretData(d.Ctx, giteaAdminNamespace, loopzeroTenantOperatorSecret,
		map[string][]byte{loopzeroTenantOperatorKey: tokenBytes}); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 3c: %v", err)}
	}

	// Layer 4: rollout restart consumers if present.
	for _, dep := range []string{minioLoopZeroAPIDeployment, loopzeroTenantOperatorDeploy} {
		if err := d.Kube.RolloutRestartDeployment(d.Ctx, giteaAdminNamespace, dep); err != nil {
			// Skip-with-INFO: if the deployment doesn't exist, bash `skip` semantics
			// (log + continue). We map to a soft continue via an error-swallow only
			// when the specific error is "not found" — otherwise it's a real failure.
			if isNotFoundErr(err) {
				continue
			}
			return Result{Verdict: FAIL, Reason: fmt.Sprintf("Layer 4 rollout %s: %v", dep, err)}
		}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("canonical Gitea token chain seeded (3 Secrets synced, token first 8=%s)", token[:min(8, len(token))])}
}

// Verify probes 3 consumer Secrets have non-empty token bytes on the canonical
// keys. Does NOT re-open a tunnel to Gitea (a separate cost). If the operator
// wants LIVE token validation, they call the seed with --verify which currently
// mirrors the assertion above (stronger drift signal — the bash also just
// re-mints and re-probes, we skip the re-mint on Verify).
func (s *GiteaTokenSeed) Verify(d Deps) Result {
	if got, err := d.Kube.GetSecret(d.Ctx, giteaAdminNamespace, giteaAdminSecretName); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get gitea-admin: %v", err)}
	} else if len(got.Data[giteaAdminTokenSecretKey]) == 0 {
		return Result{Verdict: FAIL, Reason: "gitea-admin.token empty"}
	}
	if got, err := d.Kube.GetSecret(d.Ctx, giteaAdminNamespace, loopzeroAPITokensSecret); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get loopzero-api-tokens: %v", err)}
	} else if len(got.Data[loopzeroAPITokensGiteaKey]) == 0 {
		return Result{Verdict: FAIL, Reason: "loopzero-api-tokens.gitea-token empty"}
	}
	if got, err := d.Kube.GetSecret(d.Ctx, giteaAdminNamespace, loopzeroTenantOperatorSecret); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get loopzero-tenant-operator: %v", err)}
	} else if len(got.Data[loopzeroTenantOperatorKey]) == 0 {
		return Result{Verdict: FAIL, Reason: "loopzero-tenant-operator.GITEA_TOKEN empty"}
	}
	return Result{Verdict: PASS, Reason: "all 3 consumer Secrets carry non-empty Gitea token"}
}

// giteaProbeAuth probes GET /api/v1/user with basic auth. HTTP 200 = OK.
func giteaProbeAuth(ctx context.Context, base, user, pw string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/user", nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth(user, pw)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(b))
	}
	return nil
}

// giteaEnsureOrg GETs /orgs/{name}. 200 = exists, 404 = create it (POST /orgs).
func giteaEnsureOrg(ctx context.Context, base, user, pw, org string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/orgs/"+org, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth(user, pw)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	code := resp.StatusCode
	_ = resp.Body.Close()
	switch code {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		body, _ := json.Marshal(map[string]string{
			"username": org, "full_name": "LoopZero Platform", "visibility": "private",
		})
		req2, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/orgs", bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		req2.Header.Set("Content-Type", "application/json")
		req2.SetBasicAuth(user, pw)
		resp2, err := (&http.Client{Timeout: 10 * time.Second}).Do(req2)
		if err != nil {
			return err
		}
		defer resp2.Body.Close()
		if resp2.StatusCode != http.StatusCreated {
			b, _ := io.ReadAll(resp2.Body)
			return fmt.Errorf("create org: HTTP %d: %s", resp2.StatusCode, snippet(b))
		}
		return nil
	default:
		return fmt.Errorf("GET /orgs/%s: HTTP %d", org, code)
	}
}

// giteaDeleteLegacyTokens iterates /users/{user}/tokens and deletes any whose
// name is `loopzero-api` or `loopzero-api-token` (canonical rotation pattern).
func giteaDeleteLegacyTokens(ctx context.Context, base, user, pw string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/api/v1/users/%s/tokens", base, user), nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth(user, pw)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("list tokens: HTTP %d", resp.StatusCode)
	}
	var tokens []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return fmt.Errorf("decode tokens: %w", err)
	}
	for _, t := range tokens {
		if t.Name == "loopzero-api" || t.Name == giteaTokenName {
			if err := giteaDeleteToken(ctx, base, user, pw, t.ID); err != nil {
				return fmt.Errorf("delete legacy token %q (id %d): %w", t.Name, t.ID, err)
			}
		}
	}
	return nil
}

// giteaDeleteToken deletes one access token. Gitea answers 204 on success; a
// token that is already gone (404) is the state the caller wanted.
func giteaDeleteToken(ctx context.Context, base, user, pw string, id int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		fmt.Sprintf("%s/api/v1/users/%s/tokens/%d", base, user, id), nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth(user, pw)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// giteaMintToken POSTs /users/{user}/tokens with the given name+scopes,
// returns the sha1 (that's what Gitea calls the raw token).
func giteaMintToken(ctx context.Context, base, user, pw, name string, scopes []string) (string, error) {
	body, _ := json.Marshal(map[string]any{"name": name, "scopes": scopes})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/api/v1/users/%s/tokens", base, user), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(user, pw)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(b))
	}
	var out struct {
		Sha1 string `json:"sha1"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Sha1 == "" {
		return "", fmt.Errorf("empty sha1 in mint response")
	}
	return out.Sha1, nil
}

// isNotFoundErr crudely detects k8s "not found" errors returned from
// controller-runtime updates (the RolloutRestartDeployment does Get+Update, so
// NotFound bubbles up as a wrapped apierrors.IsNotFound-shaped error).
func isNotFoundErr(err error) bool {
	// Typed check (apierrors.IsNotFound handles nil + unwrapping) — replaces the
	// former fragile err.Error() substring match. The Get+Update path returns a
	// proper *apierrors.StatusError.
	return apierrors.IsNotFound(err)
}
