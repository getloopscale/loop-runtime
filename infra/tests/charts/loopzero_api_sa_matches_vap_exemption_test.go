// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The ServiceAccount the PUBLISHED chart renders must be the one the admission
// policies exempt — and it must be so for ANY release name.
//
// # B-096
//
// Until 2026-08-19 `charts/loopzero-api/values.yaml` left `serviceAccount.name: ""`,
// so the name derived from the release: `helm template t charts/loopzero-api`
// rendered `t-loopzero-api`. The VAPs exempt
// `system:serviceaccount:loopzero-system:loopzero-api`. A deploy through the published
// chart brought loopzero-api up with an identity the platform's own fences treat as
// foreign, and the failure surfaced as `Forbidden` during reconciliation — not
// as an install error.
//
// The test renders under TWO release names on purpose. With one, it would pass
// both with the fixed name and with a values.yaml that happened to derive the
// right name from that release — and the second is no guarantee at all.
func TestPublishedChartServiceAccountMatchesTheVAPExemption(t *testing.T) {
	root := repoRoot(t)

	exempt := serviceAccountsExemptedByVAPs(t, root)
	if len(exempt) == 0 {
		t.Fatal("no exempt ServiceAccount found in the VAPs — a sweep that reaches nothing " +
			"reports green over nothing, and this whole test depends on it")
	}

	for _, release := range []string{"loopzero", "any-release-name"} {
		out := renderChart(t, publishedChart(t, "loopzero-api"), release, nil)
		sa := firstValue(out, "serviceAccountName:")
		if sa == "" {
			t.Fatalf("release %q: the render declares no serviceAccountName", release)
		}
		if !exempt[sa] {
			t.Errorf("release %q: the published chart renders ServiceAccount %q, and the VAPs exempt %v.\n"+
				"A deploy through this chart would bring loopzero-api up with an identity the platform's "+
				"own fences treat as foreign — the failure shows as Forbidden during reconciliation, "+
				"not as an install error (B-096).", release, sa, keys(exempt))
		}
	}
}

// serviceAccountsExemptedByVAPs reads the exemptions declared in the policy
// templates. It reads the SOURCE instead of rendering: the list is literal in
// the files, and rendering would need the overlay's values — more parts for the
// same answer.
func serviceAccountsExemptedByVAPs(t *testing.T, root string) map[string]bool {
	t.Helper()
	dir := filepath.Join(root, "infra", "helm", "admission-policies", "templates")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	exempt := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "vap-") {
			continue
		}
		path := filepath.Clean(filepath.Join(dir, e.Name()))
		b, err := os.ReadFile(path)
		if err != nil {
			// An unreadable policy is one whose exemptions this sweep never saw.
			t.Fatalf("reading %s: %v", path, err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			const prefix = "system:serviceaccount:loopzero-system:"
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
