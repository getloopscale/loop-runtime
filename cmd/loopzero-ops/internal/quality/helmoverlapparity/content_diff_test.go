// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmoverlapparity

import (
	"os"
	"path/filepath"
	"testing"
)

func writeValues(t *testing.T, dir, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "values.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestContentDiff_FlagsResourceDrift: a resources.requests.cpu mismatch (a field
// that DOES live in values.yaml) is a Violation. securityContext moved to
// rendered_diff.go (it is absent from real values.yaml — review finding).
func TestContentDiff_FlagsResourceDrift(t *testing.T) {
	tmp := t.TempDir()
	b := writeValues(t, filepath.Join(tmp, "b"), "resources:\n  requests:\n    cpu: \"500m\"\nimage:\n  repository: local/loopzero-api\n")
	p := writeValues(t, filepath.Join(tmp, "p"), "resources:\n  requests:\n    cpu: \"250m\"\nimage:\n  repository: oci.example/loopzero-api\n")

	vs := diffChartContent(b, p, "loopzero-api")
	if len(vs) != 1 {
		t.Fatalf("got %d violations, want 1 (resources.requests.cpu drift)", len(vs))
	}
	if vs[0].BootstrapVersion != "500m" || vs[0].PublishedVersion != "250m" {
		t.Errorf("violation should name both values: got %q vs %q", vs[0].BootstrapVersion, vs[0].PublishedVersion)
	}
}

// TestContentDiff_AllowlistedFieldIgnored: image.repository is by-design
// divergent (bootstrap=local, published=OCI) — not a violation.
func TestContentDiff_AllowlistedFieldIgnored(t *testing.T) {
	tmp := t.TempDir()
	b := writeValues(t, filepath.Join(tmp, "b"), "resources:\n  requests:\n    cpu: \"500m\"\nimage:\n  repository: local/loopzero-api\n")
	p := writeValues(t, filepath.Join(tmp, "p"), "resources:\n  requests:\n    cpu: \"500m\"\nimage:\n  repository: oci.example/loopzero-api\n")

	vs := diffChartContent(b, p, "loopzero-api")
	if len(vs) != 0 {
		t.Errorf("got %d violations, want 0 (image.repository is allowlisted by-design divergent)", len(vs))
	}
}

// TestContentDiff_PassesWhenIdentical: identical target fields → no violation.
func TestContentDiff_PassesWhenIdentical(t *testing.T) {
	tmp := t.TempDir()
	body := "resources:\n  requests:\n    cpu: \"500m\"\nimage:\n  repository: x\n"
	b := writeValues(t, filepath.Join(tmp, "b"), body)
	p := writeValues(t, filepath.Join(tmp, "p"), body)
	if vs := diffChartContent(b, p, "loopzero-api"); len(vs) != 0 {
		t.Errorf("got %d violations, want 0 (identical)", len(vs))
	}
}

// TestContentDiff_OneSideMissing (EC-2): a values.yaml missing on one side is a
// Violation chart_missing_one_side — never a panic, never a silent false
// negative.
func TestContentDiff_OneSideMissing(t *testing.T) {
	tmp := t.TempDir()
	b := writeValues(t, filepath.Join(tmp, "b"), "resources:\n  requests:\n    cpu: \"500m\"\n")
	missing := filepath.Join(tmp, "p", "values.yaml") // never created

	vs := diffChartContent(b, missing, "loopzero-api")
	if len(vs) != 1 || vs[0].Reason == "" {
		t.Fatalf("got %d violations, want 1 chart_missing_one_side", len(vs))
	}
	if !contains(vs[0].Reason, "missing") {
		t.Errorf("reason = %q, want it to mention missing (EC-2)", vs[0].Reason)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && indexOf(s, sub) >= 0))
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// M14 gap #3 fix (review finding review-tests-wiring MEDIUM): securityContext is
// hardcoded in templates/deployment.yaml, ABSENT from values.yaml, so a
// values.yaml diff can never catch runAsUser drift. These tests operate on
// RENDERED helm output (the plan's original renderFields intent), scoped to
// Deployment pod securityContexts.

// TestRenderedSecurityContext_FlagsDrift: two rendered sides whose Deployment
// pod securityContext differs → a violation.
func TestRenderedSecurityContext_FlagsDrift(t *testing.T) {
	bootstrap := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: loopzero-api
spec:
  template:
    spec:
      securityContext:
        runAsUser: 1000
        runAsNonRoot: true
`
	published := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: loopzero-api
spec:
  template:
    spec:
      securityContext:
        runAsUser: 1001
        runAsNonRoot: true
`
	vs := diffRenderedSecurityContext(bootstrap, published, "loopzero-api")
	if len(vs) != 1 {
		t.Fatalf("got %d violations, want 1 (runAsUser 1000 vs 1001 in rendered Deployment)", len(vs))
	}
}

// TestRenderedSecurityContext_PassesWhenIdentical: identical rendered pod
// securityContext → no violation (the real loopzero-api case: both render 1000/true).
func TestRenderedSecurityContext_PassesWhenIdentical(t *testing.T) {
	same := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: loopzero-api
spec:
  template:
    spec:
      securityContext:
        runAsUser: 1000
        runAsNonRoot: true
`
	if vs := diffRenderedSecurityContext(same, same, "loopzero-api"); len(vs) != 0 {
		t.Errorf("got %d violations, want 0 (identical rendered securityContext)", len(vs))
	}
}

// TestRenderedSecurityContext_DetectsMissingSecurityContextOnOneSide: a
// Deployment that lost its pod securityContext on one side is a drift (a
// non-root guarantee silently dropped) — the exact regression the gate must
// catch, and the one the values.yaml approach could NEVER see.
func TestRenderedSecurityContext_DetectsMissingSecurityContextOnOneSide(t *testing.T) {
	withCtx := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: loopzero-api
spec:
  template:
    spec:
      securityContext:
        runAsUser: 1000
        runAsNonRoot: true
`
	withoutCtx := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: loopzero-api
spec:
  template:
    spec:
      containers:
        - name: c
`
	vs := diffRenderedSecurityContext(withCtx, withoutCtx, "loopzero-api")
	if len(vs) != 1 {
		t.Fatalf("got %d violations, want 1 (securityContext present on one side only)", len(vs))
	}
}
