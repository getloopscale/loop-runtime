// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package seed

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ArgoCDGiteaCredsSeed is the Go port of
// infra/scripts/local/seed-argocd-gitea-creds-canonical.sh.
//
// It materializes argocd/argocd-creds-gitea (a Secret with the
// `argocd.argoproj.io/secret-type: repo-creds` label + repo-creds keys)
// from the canonical loopzero-system/gitea-admin Secret (username + token).
// This is what lets ArgoCD's repo-server clone the tenant GitOps manifests
// from Gitea.
//
// Parity contract (D3):
//   - loopzero-system/gitea-admin missing / .username empty  → FAIL
//     (bash `fail "gitea-admin.username missing — run task setup"`)
//   - loopzero-system/gitea-admin.token empty                → FAIL
//     (bash `fail "gitea-admin.token missing — run seed-gitea-token-canonical first"`)
//   - k8s API error                                      → FAIL
//   - success (create OR update repo-creds Secret)       → PASS
//
// Verify probes the argocd-creds-gitea Secret exists AND its username +
// password data match the current gitea-admin Secret (drift catches).
type ArgoCDGiteaCredsSeed struct{}

// NewArgoCDGiteaCredsSeed returns the seed instance for the registry.
func NewArgoCDGiteaCredsSeed() *ArgoCDGiteaCredsSeed { return &ArgoCDGiteaCredsSeed{} }

// Name is the CLI token: `loopzero-ops seed argocd-gitea-creds`.
func (s *ArgoCDGiteaCredsSeed) Name() string { return "argocd-gitea-creds" }

const (
	giteaAdminNamespace      = "loopzero-system"
	giteaAdminSecretName     = "gitea-admin"
	giteaAdminUserKey        = "username"
	giteaAdminTokenKey       = "token"
	argocdNamespace          = "argocd"
	argocdRepoCredsName      = "argocd-creds-gitea"
	argocdRepoCredsTypeLabel = "argocd.argoproj.io/secret-type"
	argocdRepoCredsTypeVal   = "repo-creds"
	argocdGiteaURL           = "http://gitea-http.loopzero-system.svc.cluster.local:3000/loopzero"
)

func (s *ArgoCDGiteaCredsSeed) Run(d Deps) Result {
	user, token, r, fail := s.readGiteaAdmin(d)
	if fail {
		return r
	}
	if d.DryRun {
		return Result{Verdict: PASS, Reason: fmt.Sprintf("dry-run: would apply %s/%s", argocdNamespace, argocdRepoCredsName)}
	}
	// B-108 — this seed runs on its own (`loopzero-ops seed argocd-gitea-creds`), which is how an operator
	// repairs one piece without repeating the whole chain. Nothing then guarantees
	// the namespace, and the failure is silent on our side: the seed stops, the
	// chain goes on, and the effect shows up in another system. Idempotent — a
	// Get when the namespace exists, which is the usual path.
	if err := d.Kube.EnsureNamespace(d.Ctx, argocdNamespace); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("ensure namespace %s: %v", argocdNamespace, err)}
	}
	secret := repoCredsSecretOf(user, token)
	if _, err := d.Kube.CreateOrUpdateSecret(d.Ctx, secret); err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("apply %s/%s: %v", argocdNamespace, argocdRepoCredsName, err)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("applied %s/%s (URL prefix %s)", argocdNamespace, argocdRepoCredsName, argocdGiteaURL)}
}

func (s *ArgoCDGiteaCredsSeed) Verify(d Deps) Result {
	user, token, r, fail := s.readGiteaAdmin(d)
	if fail {
		return r
	}
	got, err := d.Kube.GetSecret(d.Ctx, argocdNamespace, argocdRepoCredsName)
	if err != nil {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("get %s/%s: %v", argocdNamespace, argocdRepoCredsName, err)}
	}
	if string(got.Data["username"]) != user || string(got.Data["password"]) != token {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s/%s diverges from gitea-admin (username or token)", argocdNamespace, argocdRepoCredsName)}
	}
	if string(got.Data["url"]) != argocdGiteaURL {
		return Result{Verdict: FAIL, Reason: fmt.Sprintf("%s/%s url = %q, want %q", argocdNamespace, argocdRepoCredsName, got.Data["url"], argocdGiteaURL)}
	}
	return Result{Verdict: PASS, Reason: fmt.Sprintf("%s/%s matches gitea-admin canonical", argocdNamespace, argocdRepoCredsName)}
}

// readGiteaAdmin fetches the mgmt/loopzero-system/gitea-admin Secret and returns
// its (username, token). Missing Secret, missing keys, or empty values are
// FAIL per bash parity ("gitea-admin.username missing — run task setup" /
// "gitea-admin.token missing — run seed-gitea-token-canonical first").
func (s *ArgoCDGiteaCredsSeed) readGiteaAdmin(d Deps) (user, token string, r Result, isFail bool) {
	sec, err := d.Kube.GetSecret(d.Ctx, giteaAdminNamespace, giteaAdminSecretName)
	if err != nil {
		return "", "", Result{Verdict: FAIL, Reason: fmt.Sprintf("get %s/%s: %v", giteaAdminNamespace, giteaAdminSecretName, err)}, true
	}
	user = string(sec.Data[giteaAdminUserKey])
	token = string(sec.Data[giteaAdminTokenKey])
	if user == "" {
		return "", "", Result{Verdict: FAIL, Reason: "gitea-admin.username missing — run task setup"}, true
	}
	if token == "" {
		return "", "", Result{Verdict: FAIL, Reason: "gitea-admin.token missing — run seed-gitea-token-canonical first"}, true
	}
	return user, token, Result{}, false
}

// repoCredsSecretOf builds the canonical argocd-creds-gitea Secret (matches
// the bash script's inline manifest). Labels mark this as an ArgoCD
// repo-creds Secret + record the managing seed (renamed to loopzero-ops).
func repoCredsSecretOf(user, token string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: argocdNamespace,
			Name:      argocdRepoCredsName,
			Labels: map[string]string{
				argocdRepoCredsTypeLabel:       argocdRepoCredsTypeVal,
				"app.kubernetes.io/managed-by": "loopzero-ops-seed-argocd-gitea-creds",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"type":     []byte("git"),
			"url":      []byte(argocdGiteaURL),
			"username": []byte(user),
			"password": []byte(token),
		},
	}
}
