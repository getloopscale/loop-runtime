// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package release

import (
	"strings"
	"testing"
)

const stagingManifestSample = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: versioned-app-api
  namespace: loop-runtime-tn-e2etest-myproject-staging
  labels:
    app: versioned-app-api
    runtime.getloopscale.com/environment: staging
    runtime.getloopscale.com/tenant: tn-e2etest
spec:
  replicas: 1
  selector:
    matchLabels:
      app: versioned-app-api
      runtime.getloopscale.com/environment: staging
  template:
    metadata:
      labels:
        app: versioned-app-api
        runtime.getloopscale.com/environment: staging
    spec:
      containers:
        - name: app
          image: registry/img:tag
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: versioned-app-api
  namespace: loop-runtime-tn-e2etest-myproject-staging
spec:
  routes:
    - kind: Rule
      match: Host(` + "`" + `tn-e2etest-versioned-app-api-staging.loop-runtime.local` + "`" + `)
      services:
        - name: versioned-app-api
          port: 80
`

// CHG-2026-04-29-DOGFOOD-REAL-E2E iter 13 — promote cross-env rewrite
// regression. Without rewriting, the production ArgoCD App ends up
// reporting resources in the staging namespace because the manifest body
// hardcodes namespace, hostname suffix and runtime.getloopscale.com/environment label.
func TestRewriteManifestsForEnv_RewritesNamespaceHostAndLabel(t *testing.T) {
	// Empty projectID == legacy call site / pre-refactor fixture (the
	// stagingManifestSample uses the old "loop-runtime-{tenant}-{env}" format).
	// The rewriter anchors on the canonical namespace (tenant, project, env). The
	// legacy two-segment candidate was deleted together with the empty-projectID fallback.
	out, err := rewriteManifestsForEnv(
		stagingManifestSample,
		"tn-e2etest",
		"myproject",
		"staging",
		"production",
		"loop-runtime.local",
	)
	if err != nil {
		t.Fatalf("rewriteManifestsForEnv failed: %v", err)
	}

	mustContain := []string{
		"namespace: loop-runtime-tn-e2etest-myproject-production",
		"runtime.getloopscale.com/environment: production",
		"tn-e2etest-versioned-app-api-production.loop-runtime.local",
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("rewritten output missing %q\nfull output:\n%s", want, out)
		}
	}
	mustNotContain := []string{
		"loop-runtime-tn-e2etest-staging",
		"runtime.getloopscale.com/environment: staging",
		"-staging.loop-runtime.local",
	}
	for _, bad := range mustNotContain {
		if strings.Contains(out, bad) {
			t.Errorf("rewritten output still contains %q\nfull output:\n%s", bad, out)
		}
	}
}

// Sponsor LIVE-caught 2026-06-18 (Staff-level Phase 1 troubleshooting):
// the canonical namespace format moved from "loop-runtime-{tenant}-{env}" to
// "loop-runtime-{tenant}-{project}-{env}" at the 2026-06-07 multi-project refactor
// ("TUDO DEVERIA esta separado por projeto"). The renderer was updated.
// rewriteManifestsForEnv was NOT — its oldNamespace/newNamespace strings
// stayed in the legacy format, so the rewriter's exact-match check
// (s == oldNs) NEVER fired against real renderer output, and the manifest
// shipped to gitops carried the FromEnv namespace verbatim. Resources
// then deployed into the preview namespace instead of production, and the
// production ArgoCD App reported them as "Synced" because that's where
// the manifest told it to look — exactly the cross-project corruption
// observed against `sample` (build-e83c0186d91cfca1).
//
// This fixture mirrors actual renderer output for a preview-pr-1 → production
// promote on project "sample" under tenant "tn-e9bb8ad57e7f3c21". Asserts the
// rewriter swaps the namespace + hostname suffix + env label cleanly even
// when project is in the middle of the namespace segment.
const previewToProductionMultiProjectSample = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: sample-app
  namespace: loop-runtime-tn-e9bb8ad57e7f3c21-sample-preview-pr-1
  labels:
    app: sample-app
    runtime.getloopscale.com/environment: preview-pr-1
    runtime.getloopscale.com/project: sample
    runtime.getloopscale.com/tenant: tn-e9bb8ad57e7f3c21
spec:
  replicas: 2
  selector:
    matchLabels:
      app: sample-app
      runtime.getloopscale.com/environment: preview-pr-1
  template:
    metadata:
      labels:
        app: sample-app
        runtime.getloopscale.com/environment: preview-pr-1
    spec:
      containers:
        - name: app
          image: registry/sample-app:tag
---
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: sample-app
  namespace: loop-runtime-tn-e9bb8ad57e7f3c21-sample-preview-pr-1
spec:
  routes:
    - kind: Rule
      match: Host(` + "`" + `tn-e9bb8ad57e7f3c21-sample-app-preview-pr-1.loop-runtime.local` + "`" + `)
      services:
        - name: sample-app
          port: 80
`

func TestRewriteManifestsForEnv_MultiProjectCanonicalNamespace(t *testing.T) {
	// Note signature evolution: rewriter MUST receive projectID to
	// reconstruct the canonical namespace. The old signature (without
	// projectID) physically cannot match the new format because it
	// lacks the segment.
	out, err := rewriteManifestsForEnv(
		previewToProductionMultiProjectSample,
		"tn-e9bb8ad57e7f3c21",
		"sample",
		"preview-pr-1",
		"production",
		"loop-runtime.local",
	)
	if err != nil {
		t.Fatalf("rewriteManifestsForEnv failed: %v", err)
	}

	mustContain := []string{
		"namespace: loop-runtime-tn-e9bb8ad57e7f3c21-sample-production",
		"runtime.getloopscale.com/environment: production",
		"tn-e9bb8ad57e7f3c21-sample-app-production.loop-runtime.local",
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("rewritten output missing %q\nfull output:\n%s", want, out)
		}
	}
	mustNotContain := []string{
		"loop-runtime-tn-e9bb8ad57e7f3c21-sample-preview-pr-1",
		"runtime.getloopscale.com/environment: preview-pr-1",
		"-preview-pr-1.loop-runtime.local",
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
		{"same env", "staging", "staging", "loop-runtime.local"},
		{"empty from", "", "production", "loop-runtime.local"},
		{"empty to", "staging", "", "loop-runtime.local"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := rewriteManifestsForEnv(stagingManifestSample, "tn-e2etest", "myproject", c.from, c.to, c.baseDom)
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
  namespace: loop-runtime-tn-e2etest-myproject-staging
data:
  ENVIRONMENT_NOTE: "this app runs in staging mode"
  WEBSITE_URL: "https://staging.example.com"
`
	out, err := rewriteManifestsForEnv(in, "tn-e2etest", "myproject", "staging", "production", "loop-runtime.local")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Namespace MUST change.
	if !strings.Contains(out, "namespace: loop-runtime-tn-e2etest-myproject-production") {
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

// TestRewriteManifestsForEnv_EmptyProjectIDFailsLoud — the fallback to the two-segment
// "loop-runtime-{tenant}-{env}" format was deleted. Measured 2026-08-08: ZERO namespaces in that
// format exist in any cluster, and the promote route always fills ProjectID from the path
// param. The fallback protected a state that does not exist — and its price was silently
// rewriting the manifests to a namespace that does not exist, sending the deploy nowhere.
func TestRewriteManifestsForEnv_EmptyProjectIDFailsLoud(t *testing.T) {
	_, err := rewriteManifestsForEnv(stagingManifestSample, "tn-e2etest", "", "staging", "production", "loop-runtime.local")
	if err == nil {
		t.Fatal("an empty projectID must fail loud: rewriting to a two-segment " +
			"namespace sends the deploy to a namespace that does not exist")
	}
}
