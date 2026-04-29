// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"strings"
	"testing"
)

const stagingManifestSample = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: versioned-app-api
  namespace: loopzero-tn-e2etest-staging
  labels:
    app: versioned-app-api
    loopzero.io/environment: staging
    loopzero.io/tenant: tn-e2etest
spec:
  replicas: 1
  selector:
    matchLabels:
      app: versioned-app-api
      loopzero.io/environment: staging
  template:
    metadata:
      labels:
        app: versioned-app-api
        loopzero.io/environment: staging
    spec:
      containers:
        - name: app
          image: registry/img:tag
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: versioned-app-api
  namespace: loopzero-tn-e2etest-staging
spec:
  routes:
    - kind: Rule
      match: Host(` + "`" + `tn-e2etest-versioned-app-api-staging.loopzero.local` + "`" + `)
      services:
        - name: versioned-app-api
          port: 80
`

// CHG-2026-04-29-DOGFOOD-REAL-E2E iter 13 — promote cross-env rewrite
// regression. Without rewriting, the production ArgoCD App ends up
// reporting resources in the staging namespace because the manifest body
// hardcodes namespace, hostname suffix and loopzero.io/environment label.
func TestRewriteManifestsForEnv_RewritesNamespaceHostAndLabel(t *testing.T) {
	out, err := rewriteManifestsForEnv(
		stagingManifestSample,
		"tn-e2etest",
		"staging",
		"production",
		"loopzero.local",
	)
	if err != nil {
		t.Fatalf("rewriteManifestsForEnv failed: %v", err)
	}

	mustContain := []string{
		"namespace: loopzero-tn-e2etest-production",
		"loopzero.io/environment: production",
		"tn-e2etest-versioned-app-api-production.loopzero.local",
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("rewritten output missing %q\nfull output:\n%s", want, out)
		}
	}
	mustNotContain := []string{
		"loopzero-tn-e2etest-staging",
		"loopzero.io/environment: staging",
		"-staging.loopzero.local",
	}
	for _, bad := range mustNotContain {
		if strings.Contains(out, bad) {
			t.Errorf("rewritten output still contains %q\nfull output:\n%s", bad, out)
		}
	}
}

// Empty/identical envs are a no-op so promote with FromEnv == ToEnv (defensive)
// or empty baseDomain doesn't surprise-mutate the manifest.
func TestRewriteManifestsForEnv_NoOps(t *testing.T) {
	cases := []struct {
		name              string
		from, to, baseDom string
	}{
		{"same env", "staging", "staging", "loopzero.local"},
		{"empty from", "", "production", "loopzero.local"},
		{"empty to", "staging", "", "loopzero.local"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := rewriteManifestsForEnv(stagingManifestSample, "tn-e2etest", c.from, c.to, c.baseDom)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if out != stagingManifestSample {
				t.Errorf("expected manifest unchanged for no-op case, got diff:\n%s", out)
			}
		})
	}
}

// rewriting is namespace-anchored: the tenant-prefixed namespace must change
// but unrelated strings that merely contain the env name (e.g. an env var
// "DEPLOY_STAGE=staging") must NOT mutate.
func TestRewriteManifestsForEnv_DoesNotMutateUnrelatedSubstrings(t *testing.T) {
	in := `apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: loopzero-tn-e2etest-staging
data:
  ENVIRONMENT_NOTE: "this app runs in staging mode"
  WEBSITE_URL: "https://staging.example.com"
`
	out, err := rewriteManifestsForEnv(in, "tn-e2etest", "staging", "production", "loopzero.local")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Namespace MUST change.
	if !strings.Contains(out, "namespace: loopzero-tn-e2etest-production") {
		t.Errorf("namespace was not rewritten: %s", out)
	}
	// Free-form data values that only mention "staging" must NOT change.
	if !strings.Contains(out, "this app runs in staging mode") {
		t.Errorf("unrelated data string was mutated:\n%s", out)
	}
	if !strings.Contains(out, "https://staging.example.com") {
		t.Errorf("unrelated URL was mutated:\n%s", out)
	}
}
