// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"testing"

	"github.com/gruntwork-io/terratest/modules/helm"

	"github.com/getloopscale/loop-runtime/infra/tests/helm/chartrender"
)

// defaultRelease is the name `helm template <chart>` uses when none is given.
// Tests that never cared about the release keep rendering under it, so moving
// them onto the facade changed nothing they assert.
const defaultRelease = "release-name"

// Every chart this package renders goes through chartrender. Shelling out to
// `helm template` directly skipped three things the facade owns: the
// ErrAmbiguousChart guard for loop-runtime-api / loop-console, the one-shot `helm dep
// update` (without it a chart with a `dependencies:` block does not render in a
// fresh clone — 19 tests here failed that way), and one missing-helm policy.

// repoRoot is the one repository-root resolver for this package. It delegates
// to chartrender, which locates the root from its own source file rather than
// from the process working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := chartrender.RepoRoot()
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

// publishedChart resolves charts/<name>/ only.
func publishedChart(t *testing.T, name string) string {
	t.Helper()
	return mustResolve(t, name, chartrender.PublishedChartPath)
}

// uniqueChart resolves a name declared in exactly one tree, and fails on a name
// declared in both (ErrAmbiguousChart) instead of picking one.
func uniqueChart(t *testing.T, name string) string {
	t.Helper()
	return mustResolve(t, name, chartrender.ChartPath)
}

func mustResolve(t *testing.T, name string, resolve func(string) (string, error)) string {
	t.Helper()
	path, err := resolve(name)
	if err != nil {
		t.Fatalf("resolve chart %q: %v", name, err)
	}
	return path
}

// renderChart renders chartPath with the given --set values and values files
// (applied in order, newest last), failing the test if helm refuses.
func renderChart(t *testing.T, chartPath, release string, set map[string]string, valuesFiles ...string) string {
	t.Helper()
	return chartrender.RenderTemplate(t, renderOptions(t, chartPath, set, valuesFiles), chartPath, release, nil)
}

// renderChartE is renderChart for a test whose assertion is that helm refuses.
func renderChartE(t *testing.T, chartPath, release string, set map[string]string, valuesFiles ...string) (string, error) {
	t.Helper()
	return chartrender.RenderTemplateE(t, renderOptions(t, chartPath, set, valuesFiles), chartPath, release, nil)
}

func renderOptions(t *testing.T, chartPath string, set map[string]string, valuesFiles []string) *helm.Options {
	t.Helper()
	opts, err := chartrender.NewOptions(chartPath, "")
	if err != nil {
		t.Fatalf("render options for %s: %v", chartPath, err)
	}
	opts.SetValues = set
	opts.ValuesFiles = valuesFiles
	return opts
}
