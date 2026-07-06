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

// TestContentDiff_FlagsRunAsUserDrift: a securityContext.runAsUser mismatch
// between the two chart copies is a Violation with both values named (M14 gap
// #3, ADR — verify-only content diff, kserve verify-helm-helpers.sh shape).
func TestContentDiff_FlagsRunAsUserDrift(t *testing.T) {
	tmp := t.TempDir()
	b := writeValues(t, filepath.Join(tmp, "b"), "securityContext:\n  runAsUser: 1000\nimage:\n  repository: local/loopzero-api\n")
	p := writeValues(t, filepath.Join(tmp, "p"), "securityContext:\n  runAsUser: 1001\nimage:\n  repository: oci.example/loopzero-api\n")

	vs := diffChartContent(b, p, "loopzero-api")
	if len(vs) != 1 {
		t.Fatalf("got %d violations, want 1 (runAsUser drift)", len(vs))
	}
	if vs[0].BootstrapVersion != "1000" || vs[0].PublishedVersion != "1001" {
		t.Errorf("violation should name both values: got %q vs %q", vs[0].BootstrapVersion, vs[0].PublishedVersion)
	}
}

// TestContentDiff_AllowlistedFieldIgnored: image.repository is by-design
// divergent (bootstrap=local, published=OCI) — not a violation.
func TestContentDiff_AllowlistedFieldIgnored(t *testing.T) {
	tmp := t.TempDir()
	b := writeValues(t, filepath.Join(tmp, "b"), "securityContext:\n  runAsUser: 1000\nimage:\n  repository: local/loopzero-api\n")
	p := writeValues(t, filepath.Join(tmp, "p"), "securityContext:\n  runAsUser: 1000\nimage:\n  repository: oci.example/loopzero-api\n")

	vs := diffChartContent(b, p, "loopzero-api")
	if len(vs) != 0 {
		t.Errorf("got %d violations, want 0 (image.repository is allowlisted by-design divergent)", len(vs))
	}
}

// TestContentDiff_PassesWhenIdentical: identical target fields → no violation.
func TestContentDiff_PassesWhenIdentical(t *testing.T) {
	tmp := t.TempDir()
	body := "securityContext:\n  runAsUser: 1000\nimage:\n  repository: x\n"
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
	b := writeValues(t, filepath.Join(tmp, "b"), "securityContext:\n  runAsUser: 1000\n")
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
