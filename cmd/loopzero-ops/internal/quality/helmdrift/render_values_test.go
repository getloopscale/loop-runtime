// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

import (
	"os/exec"
	"path/filepath"
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
	got := helmTemplateArgs("charts/loopzero-api", rbacValuesMatrix[1])
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

	on, err := helmRenderer{}.Render(chart, rbacValuesMatrix[0])
	if err != nil {
		t.Fatalf("render at matrix point 0: %v", err)
	}
	off, err := helmRenderer{}.Render(chart, rbacValuesMatrix[1])
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
