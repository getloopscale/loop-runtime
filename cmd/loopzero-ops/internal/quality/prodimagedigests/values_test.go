// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package prodimagedigests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScanChartValues closes the gap this gate DOCUMENTED and never checked.
//
// The template scanner deliberately treats `{{ include "loopzero-common.image" . }}`
// as clean, and its own comment says why: *"the violation surfaces at the
// values.yaml level when digest is missing"*. That sentence names, exactly, a
// check that was never written.
//
// Measured 2026-08-16: all SEVEN published charts use the helper, none declares
// `image.digest`, and all seven declare `tag: dev`. The helper prefers digest and
// falls back to the tag, so the effective default reference of every published
// chart is a mutable `loopzero-x:dev` — and the gate reported PASS on 66 files while
// the only three findings were Helm test pods.
//
// The chart does the right thing in the template layer and the wrong thing in the
// values layer, and the gate only looked at one of them.
func TestScanChartValues(t *testing.T) {
	cases := []struct {
		name       string
		values     string
		usesHelper bool
		wantHit    bool
		wantIn     string
	}{
		{
			name:       "helper without digest is the shape the gate did not see",
			values:     "image:\n  repository: loopzero-api\n  tag: dev\n",
			usesHelper: true, wantHit: true, wantIn: "digest",
		},
		{
			name:       "a declared digest is immutable, so it passes",
			values:     "image:\n  repository: loopzero-api\n  digest: sha256:abc123\n",
			usesHelper: true, wantHit: false,
		},
		{
			name: "an empty digest does not count as declared — the helper falls back to the tag",
			// `.Values.image.digest | default ""` treats empty as absent, and
			// accepting the key-present-but-empty form would accept the appearance
			// of the fix in place of the fix.
			values:     "image:\n  repository: loopzero-api\n  digest: \"\"\n  tag: dev\n",
			usesHelper: true, wantHit: true, wantIn: "digest",
		},
		{
			name: "a chart that does NOT use the helper is not judged here",
			// Whoever writes a literal `image:` is already covered by the template
			// scanner. Judging twice would produce two findings for one defect,
			// and the reader learns to add wrong.
			values:     "image:\n  repository: anything\n  tag: v1\n",
			usesHelper: false, wantHit: false,
		},
		{
			name:       "no image block at all is not a violation",
			values:     "replicaCount: 2\n",
			usesHelper: true, wantHit: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			chart := filepath.Join(root, "charts", "loopzero-fixture")
			if err := os.MkdirAll(filepath.Join(chart, "templates"), 0o755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
			if err := os.WriteFile(filepath.Join(chart, "values.yaml"), []byte(tc.values), 0o600); err != nil {
				t.Fatalf("WriteFile values: %v", err)
			}
			tmpl := "spec:\n  containers:\n    - image: nginx@sha256:deadbeef\n"
			if tc.usesHelper {
				tmpl = "spec:\n  containers:\n    - image: {{ include \"loopzero-common.image\" . }}\n"
			}
			if err := os.WriteFile(filepath.Join(chart, "templates", "deployment.yaml"), []byte(tmpl), 0o600); err != nil {
				t.Fatalf("WriteFile template: %v", err)
			}

			var out Outcome
			if err := scanChartValues(filepath.Join(root, "charts"), &out); err != nil {
				t.Fatalf("scanChartValues: %v", err)
			}

			if tc.wantHit && len(out.Violations) == 0 {
				t.Fatalf("expected a violation, got none")
			}
			if !tc.wantHit && len(out.Violations) > 0 {
				t.Fatalf("expected no violation, got %d: %v", len(out.Violations), out.Violations)
			}
			if tc.wantHit && tc.wantIn != "" && !strings.Contains(joinViolations(out.Violations), tc.wantIn) {
				t.Errorf("the violation does not name the cause (%q): %v", tc.wantIn, out.Violations)
			}
		})
	}
}

// TestScanChartValues_MissingDirectoryIsNotAnError keeps the gate usable in a
// checkout that has no `charts/` — same tolerance the path walker already has.
func TestScanChartValues_MissingDirectoryIsNotAnError(t *testing.T) {
	var out Outcome
	if err := scanChartValues(filepath.Join(t.TempDir(), "does-not-exist"), &out); err != nil {
		t.Fatalf("a missing directory became an error: %v", err)
	}
	if len(out.Violations) != 0 {
		t.Errorf("a missing directory produced violations: %v", out.Violations)
	}
}

// joinViolations renders every finding for substring assertions. The rendering is
// the operator-facing form, so asserting against it also guards the report.
func joinViolations(vs []Violation) string {
	parts := make([]string, 0, len(vs))
	for _, v := range vs {
		parts = append(parts, v.String())
	}
	return strings.Join(parts, " ")
}

// A chart template the gate cannot read must not decide, by silence, that the
// chart does not use the image helper — that would drop the chart from the
// digest check.
func TestScanChartValues_UnreadableTemplateIsAnError(t *testing.T) {
	root := t.TempDir()
	chartDir := filepath.Join(root, "charts", "api")
	if err := os.MkdirAll(filepath.Join(chartDir, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "values.yaml"), []byte("image:\n  tag: dev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing-target"), filepath.Join(chartDir, "templates", "deploy.yaml")); err != nil {
		t.Fatal(err)
	}

	var out Outcome
	if err := scanChartValues(filepath.Join(root, "charts"), &out); err == nil {
		t.Fatal("an unreadable template must be an error")
	}
}
