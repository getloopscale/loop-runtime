// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billingsecrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

// The quietest of the four layers.
//
// MEASURED on 2026-08-11: the dashboard pod ran 20h `Running` without the two billing
// Secrets, and did not emit a single event. The seed at least prints SKIP; the runtime at
// least logs a line at boot. `optional: true` leaves no trace at all — it is what turns
// "we have no credential" into "started normally".
//
// The gate does NOT require an unconditional `optional: false`: a hermetic environment
// mounts without a credential on purpose. What it requires is that the decision be a
// VALUE, not a literal — because a literal cannot differ by environment, and that is how
// it ends up being `true` in all of them.

func writeChart(t *testing.T, root, rel, body string) {
	t.Helper()
	c := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(c), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

const literalMount = `
            - secretRef:
                name: loopzero-stripe-webhook
                optional: true
            - secretRef:
                name: loopzero-stripe-api-keys
                optional: true
`

const valueMount = `
            - secretRef:
                name: loopzero-stripe-webhook
                optional: {{ not .Values.billing.required }}
            - secretRef:
                name: loopzero-stripe-api-keys
                optional: {{ not .Values.billing.required }}
`

// TestFailsLiteralOptionalMount is the case that fails today.
func TestFailsLiteralOptionalMount(t *testing.T) {
	root := t.TempDir()
	writeChart(t, root, "infra/helm/loopzero-cloud/templates/deployment.yaml", literalMount)

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.Findings) != 2 {
		t.Fatalf("both billing secrets are fixed as optional; wanted 2 findings, got %d: %+v",
			len(out.Findings), out.Findings)
	}
	if !strings.Contains(strings.ToLower(out.Findings[0].Why), "optional") {
		t.Errorf("the reason must name what is wrong; got %q", out.Findings[0].Why)
	}
}

// TestAcceptsMountDecidedByValue — the correct form passes.
func TestAcceptsMountDecidedByValue(t *testing.T) {
	root := t.TempDir()
	writeChart(t, root, "infra/helm/loopzero-cloud/templates/deployment.yaml", valueMount)

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.Findings) != 0 {
		t.Errorf("a mount decided by value can be optional in the hermetic environment and "+
			"mandatory in the public one; it should not fail: %+v", out.Findings)
	}
}

// TestScansBothSiblingCharts pins the overlapping-charts rule.
//
// `CLAUDE.md § Helm chart dual system`: `loopzero-cloud` exists in `infra/helm/` and in
// `charts/`, and fixing only one leaves the other publishing the defect. A gate that looked
// only at the first would pass while the sibling stays broken — and the sibling is
// precisely the one published via OCI, which is what consumers install.
func TestScansBothSiblingCharts(t *testing.T) {
	root := t.TempDir()
	writeChart(t, root, "infra/helm/loopzero-cloud/templates/deployment.yaml", valueMount)
	writeChart(t, root, "charts/loopzero-cloud/templates/deployment.yaml", literalMount)

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.Findings) != 2 {
		t.Fatalf("the published sibling is still literal; wanted 2 findings from charts/, got %d",
			len(out.Findings))
	}
	for _, f := range out.Findings {
		if !strings.Contains(f.File, "charts/loopzero-cloud") {
			t.Errorf("the finding should point at the broken sibling, got %q", f.File)
		}
	}
}

// TestNoChartInventsNoFinding — an absent file is not a violation.
func TestNoChartInventsNoFinding(t *testing.T) {
	out, err := Check(Config{RepoRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("a repository without the chart is not an error: %v", err)
	}
	if len(out.Findings) != 0 {
		t.Errorf("without a chart there is nothing to fail, got %+v", out.Findings)
	}
}

// TestRealRepositoryPasses is the gate against the real repository.
//
// It failed when it was written, on purpose: it was the proof that the measured defect
// existed and that the gate saw it. It passes now that the literal optionality is gone,
// and it guards against its return.
func TestRealRepositoryPasses(t *testing.T) {
	root := qtestutil.Root(t)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check on the real repository: %v", err)
	}
	for _, f := range out.Findings {
		t.Errorf("%s:%d — %s", f.File, f.Line, f.Why)
	}
}
