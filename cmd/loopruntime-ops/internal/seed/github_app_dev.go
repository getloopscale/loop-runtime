// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package seed

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
)

// GitHubAppDevSeed is the Go port of infra/scripts/local/seed-github-app-dev.sh.
// It materializes two K8s Secrets in loopruntime-system that loopruntime-api consumes via
// secrets.additionalSources and secrets.keyRefs (see charts/loopruntime-api/values-bootstrap.yaml):
//
//	loopruntime-api-github     key=GITHUB_APP_PRIVATE_KEY   (PKCS1 RSA PEM)
//	loopruntime-github-webhook key=value                    (HMAC-SHA256 shared secret)
//
// Semantics preserved from the bash seed (parity contract per D3):
//   - creds absent (Deps.Creds == nil)              → SKIP
//   - github.app_id / private_key / webhook_secret empty → SKIP (placeholder-graceful)
//   - private_key not a valid PKCS1 RSA PEM        → FAIL (matches
//     api/internal/github/appclient.go:75 ParsePKCS1PrivateKey expectation)
//   - k8s API error                                → FAIL
//   - success (create OR update)                   → PASS
//
// Verify probes the post-condition: both Secrets exist with data matching
// the creds file. Stronger than the bash `--verify` (which grepped a startup
// log line): a content-match Verify is deterministic and requires no rollout.
type GitHubAppDevSeed struct{}

// NewGitHubAppDevSeed returns the seed instance for the registry.
func NewGitHubAppDevSeed() *GitHubAppDevSeed { return &GitHubAppDevSeed{} }

// Name is the CLI token: `loopruntime-ops seed github-app-dev`.
func (s *GitHubAppDevSeed) Name() string { return "github-app-dev" }

const (
	githubAppNamespace   = "loopruntime-system"
	githubAppSecretName  = "loopruntime-api-github"
	githubAppSecretKey   = "GITHUB_APP_PRIVATE_KEY"
	githubWebhookName    = "loopruntime-github-webhook"
	githubWebhookDataKey = "value"
)

// Run applies both Secrets idempotently. DryRun mode returns PASS without mutation.
func (s *GitHubAppDevSeed) Run(d Deps) Result {
	if r, skip := s.precondition(d); skip {
		return r
	}
	if d.DryRun {
		return Result{Verdict: PASS, Reason: "dry-run: would apply loopruntime-api-github + loopruntime-github-webhook"}
	}
	// Byte-for-byte parity with the bash seed: `$(python3 ...)` expansion
	// strips trailing newlines, then `printf "%s" > /tmp` writes without a
	// terminator. Preserve that shape so the K8s Secret data is identical.
	// The namespace before the Secret that lives in it.
	//
	// B-021: on a fresh cleanroom this seed ran before `loopruntime-system` existed and
	// failed on the prerequisite, so the webhook secret was never written, so the
	// handler stayed nil, so the route was never registered — and the only symptom
	// was a 404 in GitHub's delivery log. The chain never announced itself.
	//
	// Idempotent, and a no-op against the fake client the unit tests use, which is
	// why the guard for this is the structural assertion in
	// namespace_precondition_test.go rather than a behavioural one here.
	if err := d.Kube.EnsureNamespace(d.Ctx, githubAppNamespace); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("ensure namespace %s: %v", githubAppNamespace, err)}
	}

	pem := []byte(strings.TrimRight(d.Creds.GitHub.AppPrivateKey, "\n"))
	webhook := []byte(strings.TrimRight(d.Creds.GitHub.WebhookSecret, "\n"))
	if _, err := d.Kube.CreateOrUpdateSecret(d.Ctx,
		secretOf(githubAppNamespace, githubAppSecretName, map[string][]byte{githubAppSecretKey: pem})); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("apply %s: %v", githubAppSecretName, err)}
	}
	if _, err := d.Kube.CreateOrUpdateSecret(d.Ctx,
		secretOf(githubAppNamespace, githubWebhookName, map[string][]byte{githubWebhookDataKey: webhook})); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("apply %s: %v", githubWebhookName, err)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("applied %s + %s in %s", githubAppSecretName, githubWebhookName, githubAppNamespace)}
}

// Verify probes both Secrets and confirms their data matches the creds file.
// Absence of either Secret, or content divergence, is FAIL (state has drifted).
func (s *GitHubAppDevSeed) Verify(d Deps) Result {
	if r, skip := s.precondition(d); skip {
		return r
	}
	got, err := d.Kube.GetSecret(d.Ctx, githubAppNamespace, githubAppSecretName)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get %s: %v", githubAppSecretName, err)}
	}
	if string(got.Data[githubAppSecretKey]) != strings.TrimRight(d.Creds.GitHub.AppPrivateKey, "\n") {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s/%s value diverges from creds", githubAppSecretName, githubAppSecretKey)}
	}
	got2, err := d.Kube.GetSecret(d.Ctx, githubAppNamespace, githubWebhookName)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get %s: %v", githubWebhookName, err)}
	}
	if string(got2.Data[githubWebhookDataKey]) != strings.TrimRight(d.Creds.GitHub.WebhookSecret, "\n") {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s/%s value diverges from creds", githubWebhookName, githubWebhookDataKey)}
	}
	return Result{Verdict: PASS, Reason: "both Secrets present with data matching creds"}
}

// precondition returns (result, true) when the seed should return early
// with SKIP or FAIL. (Result{}, false) means proceed to mutation.
func (s *GitHubAppDevSeed) precondition(d Deps) (Result, bool) {
	if d.Creds == nil {
		return Result{Verdict: SKIP, Reason: "dev-credentials.json absent (SKIP-graceful)"}, true
	}
	g := d.Creds.GitHub
	if g.AppID == "" || g.AppPrivateKey == "" || g.WebhookSecret == "" {
		return Result{Verdict: SKIP, Reason: "github.{app_id,app_private_key,webhook_secret} missing/empty"}, true
	}
	// PEM must parse as PKCS1 RSA — mirrors api/internal/github/appclient.go:75
	// ParsePKCS1PrivateKey. Bash counterpart uses `openssl rsa -noout -check`.
	block, _ := pem.Decode([]byte(g.AppPrivateKey))
	if block == nil {
		return Result{Verdict: FAIL, Reason: "github.app_private_key: not a PEM block"}, true
	}
	if _, err := x509.ParsePKCS1PrivateKey(block.Bytes); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("github.app_private_key: PKCS1 parse failed: %v", err)}, true
	}
	return Result{}, false
}
