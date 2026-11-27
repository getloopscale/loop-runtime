// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package chartdefaulttags

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeChart(t *testing.T, root, name, values string) {
	t.Helper()
	d := filepath.Join(root, "infra", "helm", name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(d, "values.yaml"), []byte(values), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestCheck(t *testing.T) {
	cases := []struct {
		name     string
		values   string
		wantPass bool
		wantTag  string
	}{
		{
			name:     "the #232 shape — default tag: latest",
			values:   "image:\n  repository: registry/loop-runtime-api\n  tag: latest\n",
			wantPass: false,
			wantTag:  "latest",
		},
		{
			name:     "stable is equally mutable",
			values:   "image:\n  tag: stable\n",
			wantPass: false,
			wantTag:  "stable",
		},
		{
			name:     "a branch name is mutable too",
			values:   "image:\n  tag: main\n",
			wantPass: false,
			wantTag:  "main",
		},
		{
			// The preferred fix: empty default fails loudly at deploy if
			// nothing overrides, instead of silently shipping whatever
			// `latest` points at today.
			name:     "empty default passes",
			values:   `image:` + "\n  tag: \"\"\n",
			wantPass: true,
		},
		{
			name:     "an immutable semver default passes",
			values:   "image:\n  tag: v1.4.2\n",
			wantPass: true,
		},
		{
			// The gate must not fire on prose. A comment mentioning the word
			// is not an assignment, and a guard that cannot tell the
			// difference gets exempted away.
			name:     "the word latest inside a comment does not fire",
			values:   "# CI overrides this; never leave it at latest\nimage:\n  tag: v2.0.0\n",
			wantPass: true,
		},
		{
			// A trailing comment on a real assignment must NOT hide it —
			// the inverse mistake, and the more dangerous one.
			name:     "a trailing comment does not hide a mutable assignment",
			values:   "image:\n  tag: latest  # overridden by CI\n",
			wantPass: false,
			wantTag:  "latest",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeChart(t, root, "loop-runtime-subject", tc.values)
			out, err := Check(Config{RepoRoot: root})
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if out.Skipped {
				t.Fatalf("unexpected skip: %s", out.Reason)
			}
			if out.Pass != tc.wantPass {
				t.Fatalf("Pass = %v, want %v (findings %+v)", out.Pass, tc.wantPass, out.Findings)
			}
			if tc.wantTag != "" {
				found := false
				for _, f := range out.Findings {
					if f.Tag == tc.wantTag {
						found = true
					}
				}
				if !found {
					t.Errorf("findings %+v do not name tag %q — the message must say WHICH value is mutable", out.Findings, tc.wantTag)
				}
			}
		})
	}
}

// TestCheck_NoChartsIsNotASilentPass — green must never mean "found nothing to look at".
func TestCheck_NoChartsIsNotASilentPass(t *testing.T) {
	out, err := Check(Config{RepoRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Skipped {
		t.Fatalf("no charts should SKIP, got Pass=%v scanned=%d", out.Pass, out.Scanned)
	}
	if !strings.Contains(out.Reason, "NOT verified") {
		t.Errorf("skip reason %q must state the defaults were not verified", out.Reason)
	}
}

// TestCheck_AgainstLiveRepo pins the CURRENT state. Expected RED until #232
// lands: the fix edits infra/helm/, a governance-gated path. The gate is
// registered but deliberately NOT wired — see the failingGate ledger entry in
// cmd/loop-runtime-ops/quality_gate_wiring_test.go.
func TestCheck_AgainstLiveRepo(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Skipped {
		t.Skipf("%s", out.Reason)
	}
	if out.Pass {
		t.Log("no mutable chart defaults — #232 appears fixed; wire this gate and drop its failingGate entry")
		return
	}
	for _, f := range out.Findings {
		t.Logf("KNOWN (#232) %s:%d — chart %s defaults to mutable tag %q", f.File, f.Line, f.Chart, f.Tag)
	}
}

// A chart Helmfile installs from charts/ takes its bootstrap defaults from
// charts/<name>/values-bootstrap.yaml, not from a copy under infra/helm. Those
// defaults are what a forgotten override ships, so they are held to the same bar.
func TestCheck_BootstrapValuesOfAPublishedChartAreChecked(t *testing.T) {
	root := t.TempDir()
	d := filepath.Join(root, "charts", "loop-runtime-subject")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(d, "values-bootstrap.yaml"), []byte("image:\n  tag: latest\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass || len(out.Findings) != 1 || out.Findings[0].File != "charts/loop-runtime-subject/values-bootstrap.yaml" {
		t.Fatalf("a mutable tag in bootstrap values must be one finding naming the file, got %+v (skipped=%v)", out.Findings, out.Skipped)
	}
}
