// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

import (
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// B-001 T2.1 RED.
//
// The RBAC comparison has to render the published chart under each entry of
// rbacValuesMatrix, and the seam it renders through cannot express that: the
// interface was `Render(chartDir string) (string, error)` and the production
// implementation shelled `helm template drift-probe <dir>` with no `--set` at
// all. These tests do not compile against that signature, which is the RED.
//
// The seam is WIDENED rather than doubled by a second interface. Two interfaces
// would mean a type assertion at the call site and a fallback path that renders
// at defaults while the caller believes it rendered a matrix — a way for
// "both flag states agree" to be reported about a renderer that ignored them.
// One method, two doubles updated, no path where the values are silently dropped.

// TestHelmTemplateArgsCarryTheSetValues pins the argument construction itself,
// with no subprocess, so the check stays hermetic: the gate must not need the
// toolchain it audits in order to test how it invokes it.
func TestHelmTemplateArgsCarryTheSetValues(t *testing.T) {
	got := helmTemplateArgs("charts/loopzero-api", matrixFor("loopzero-api:loopzero-api")[1])
	joined := strings.Join(got, " ")

	for _, want := range []string{
		"template", "drift-probe", "charts/loopzero-api",
		"--set rbac.create=true", "--set rbac.legacyClusterWideSecrets=false",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("helm args %q do not carry %q", joined, want)
		}
	}

	t.Run("no values means the invocation is unchanged", func(t *testing.T) {
		// The kind-level comparison renders at DEFAULTS and must keep doing so:
		// its eleven approvals were measured under them, and a gate that changed
		// its own baseline while adding a second comparison would make both
		// findings unreadable.
		bare := helmTemplateArgs("charts/loopzero-api", nil)
		if strings.Join(bare, " ") != "template drift-probe charts/loopzero-api" {
			t.Errorf("a valueless render must be the original invocation, got %q", bare)
		}
	})
}

// TestMatrixPointsProduceDifferentPublishedOutput proves the `--set` flags reach
// helm and change what it renders. An argument list that is built correctly and
// never applied would pass the test above and measure nothing.
//
// This one needs `helm`, because the production renderer IS helm. It fails rather
// than skipping: a skip here would report green having measured nothing, which is
// the failure class B-001 exists to remove.
func TestMatrixPointsProduceDifferentPublishedOutput(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatalf("helm is not on PATH and this test measures the production renderer: %v", err)
	}
	chart := filepath.Join("..", "..", "..", "..", "..", "charts", "loopzero-api")

	on, err := helmRenderer{}.Render(chart, matrixFor("loopzero-api:loopzero-api")[0])
	if err != nil {
		t.Fatalf("render at matrix point 0: %v", err)
	}
	off, err := helmRenderer{}.Render(chart, matrixFor("loopzero-api:loopzero-api")[1])
	if err != nil {
		t.Fatalf("render at matrix point 1: %v", err)
	}
	if on == off {
		t.Fatal("the two matrix points rendered byte-identical output — the --set flags " +
			"are being built and dropped, so the matrix would report agreement it never measured")
	}
}

// TestRenderErrorNamesTheChartAndTheSetList is the negative case: a failure has
// to carry enough for a reader to reproduce the exact invocation, not merely
// report that something went wrong (rules/error-handling.md § 2).
func TestRenderErrorNamesTheChartAndTheSetList(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatalf("helm is not on PATH and this test measures the production renderer: %v", err)
	}
	_, err := helmRenderer{}.Render(t.TempDir(), []string{"rbac.create=true"})
	if err == nil {
		t.Fatal("rendering a directory that is not a chart succeeded")
	}
	if !strings.Contains(err.Error(), "rbac.create=true") {
		t.Errorf("the error does not name the --set list, so the invocation cannot be "+
			"reproduced from it: %v", err)
	}
}

// B-001 T5.1. The matrix is the gate's own configuration and the reported count
// rests on it, so it is asserted rather than merely written down.
func TestTheValuesMatrixIsTheTwoMeasuredPoints(t *testing.T) {
	got := matrixFor("loopzero-api:loopzero-api")
	if len(got) != 2 {
		t.Fatalf("expected the two measured points, got %v", got)
	}
	var flags []string
	for _, entry := range got {
		joined := strings.Join(entry, " ")
		if !strings.Contains(joined, "rbac.create=true") {
			t.Errorf("entry %q does not enable rbac.create, so the published chart renders no "+
				"ClusterRole at all under it", entry)
		}
		flags = append(flags, joined)
	}

	// Both values of the flag are named EXPLICITLY, which is the whole point of
	// pinning: the reported count cannot move because somebody flipped the
	// default in values.yaml, only because a chart's rules changed (NFR-006).
	sort.Strings(flags)
	if !strings.Contains(flags[0], "rbac.legacyClusterWideSecrets=false") ||
		!strings.Contains(flags[1], "rbac.legacyClusterWideSecrets=true") {
		t.Errorf("the matrix does not pin both states of rbac.legacyClusterWideSecrets: %v", flags)
	}
}

// Negative case: the degenerate configuration must not be reintroduced silently.
//
// At rbac.create=false the published chart renders no ClusterRole — helm reports
// "could not find template templates/clusterrole.yaml in chart" — so every
// comparison degenerates to "all 175 bootstrap cells diverge". A number printed
// forever is how a gate teaches everyone to ignore it.
func TestTheMatrixExcludesRbacCreateFalse(t *testing.T) {
	for pair, entries := range rbacValuesMatrix {
		for _, entry := range entries {
			for _, set := range entry {
				if strings.Contains(strings.ReplaceAll(set, " ", ""), "rbac.create=false") {
					t.Errorf("%s pins %q; at that value the published chart renders no "+
						"ClusterRole and the comparison reports total divergence forever", pair, set)
				}
			}
		}
	}
}

// A pair the matrix does not name is rendered once, at whatever the chart itself
// defaults to. Measured 2026-09-15: charts/loopzero-admission-policies ships a
// values.schema.json and REJECTS `--set rbac.create=true` outright, so a
// gate-wide matrix would have made two of the three pairs unrenderable.
func TestAPairWithNoMatrixRendersOnceAtChartDefaults(t *testing.T) {
	got := matrixFor("loopzero-admission-policies:admission-policies")
	if len(got) != 1 || got[0] != nil {
		t.Errorf("expected a single render at chart defaults, got %v", got)
	}
}
