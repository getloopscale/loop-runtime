// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"strings"
	"testing"
)

// The ServiceAccount the PUBLISHED chart renders must be the one the admission
// policies exempt — and it must be so for ANY release name.
//
// # B-096
//
// Until 2026-08-19 `charts/loop-runtime-api/values.yaml` left `serviceAccount.name: ""`,
// so the name derived from the release: `helm template t charts/loop-runtime-api`
// rendered `t-loop-runtime-api`. The VAPs exempt
// `system:serviceaccount:loop-runtime-system:loop-runtime-api`. A deploy through the published
// chart brought loop-runtime-api up with an identity the platform's own fences treat as
// foreign, and the failure surfaced as `Forbidden` during reconciliation — not
// as an install error.
//
// The test renders under TWO release names on purpose. With one, it would pass
// both with the fixed name and with a values.yaml that happened to derive the
// right name from that release — and the second is no guarantee at all.
func TestPublishedChartServiceAccountMatchesTheVAPExemption(t *testing.T) {
	exempt := serviceAccountsExemptedByVAPs(t)
	if len(exempt) == 0 {
		t.Fatal("no exempt ServiceAccount found in the VAPs — a sweep that reaches nothing " +
			"reports green over nothing, and this whole test depends on it")
	}

	for _, release := range []string{"loop-runtime", "any-release-name"} {
		out := renderChart(t, publishedChart(t, "loop-runtime-api"), release, nil)
		sa := firstValue(out, "serviceAccountName:")
		if sa == "" {
			t.Fatalf("release %q: the render declares no serviceAccountName", release)
		}
		if !exempt[sa] {
			t.Errorf("release %q: the published chart renders ServiceAccount %q, and the VAPs exempt %v.\n"+
				"A deploy through this chart would bring loop-runtime-api up with an identity the platform's "+
				"own fences treat as foreign — the failure shows as Forbidden during reconciliation, "+
				"not as an install error (B-096).", release, sa, keys(exempt))
		}
	}
}

// serviceAccountsExemptedByVAPs returns the loop-runtime-system ServiceAccounts the
// admission policies exempt, read from the RENDERED chart. The list used to be
// a literal in each template; it now lives in values.yaml and is expanded by the
// loop-runtime-admission.systemActorExemptions helper, so only a render sees it. Every
// policy that carries the exemption is switched on, so none is missed because
// its toggle defaults off.
func serviceAccountsExemptedByVAPs(t *testing.T) map[string]bool {
	t.Helper()
	out := renderChart(t, publishedChart(t, "loop-runtime-admission-policies"), "exemptions", map[string]string{
		"vaps.protectPlatformDeletes":         "true",
		"vaps.protectPlatformNetworkpolicies": "true",
		"vaps.tenantCrShape":                  "true",
		"vaps.tenantNamespace":                "true",
	})
	exempt := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		const prefix = "system:serviceaccount:loop-runtime-system:"
		i := strings.Index(line, prefix)
		if i < 0 {
			continue
		}
		rest := line[i+len(prefix):]
		end := strings.IndexAny(rest, "'\",: \t")
		if end < 0 {
			end = len(rest)
		}
		if name := rest[:end]; name != "" {
			exempt[name] = true
		}
	}
	return exempt
}

func firstValue(rendered, key string) string {
	for _, line := range strings.Split(rendered, "\n") {
		if i := strings.Index(line, key); i >= 0 {
			return strings.TrimSpace(line[i+len(key):])
		}
	}
	return ""
}

func keys(m map[string]bool) []string {
	var k []string
	for s := range m {
		k = append(k, s)
	}
	return k
}
