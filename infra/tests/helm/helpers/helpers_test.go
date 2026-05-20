//go:build helm_render

// Package helpers self-tests.
//
// Plan 2026-05-20-helm-render-time-testing-terratest v1.1 — T1.2 TDD.
//
// Per skill rules: tests written FIRST (RED), implementation later (GREEN).
// All tests in this file validate the helpers package contract.
package helpers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestChartPath_resolves_known_charts ensures both infra/helm/ and
// charts/ lookups work.
func TestChartPath_resolves_known_charts(t *testing.T) {
	t.Parallel()

	// Note: ChartPath prefers infra/helm/ first per CLAUDE.md Dual System.
	// loopzero-api exists in BOTH infra/helm/ and charts/ — infra/helm wins.
	// postgresql + cert-manager are upstream-wrappers (no Chart.yaml) —
	// they would be auto-exempt per coverage gate, not testable here.
	cases := []struct {
		name      string
		chartName string
		wantDir   string // expected to contain this segment
	}{
		{"loopzero-api: infra/helm/ takes precedence over charts/", "loopzero-api", "infra/helm/loopzero-api"},
		{"chaos-mesh lives in infra/helm/ with Chart.yaml", "chaos-mesh", "infra/helm/chaos-mesh"},
		{"loopzero-app-operator lives in charts/", "loopzero-app-operator", "charts/loopzero-app-operator"},
		{"loopzero-common (library) lives in charts/", "loopzero-common", "charts/loopzero-common"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ChartPath(tc.chartName)
			if err != nil {
				t.Fatalf("ChartPath(%q) error: %v", tc.chartName, err)
			}
			if !strings.Contains(got, tc.wantDir) {
				t.Errorf("ChartPath(%q) = %q; want path containing %q",
					tc.chartName, got, tc.wantDir)
			}
			// Confirm Chart.yaml actually exists
			if _, err := os.Stat(filepath.Join(got, "Chart.yaml")); err != nil {
				t.Errorf("Chart.yaml not at %s: %v", got, err)
			}
		})
	}
}

// TestChartPath_rejects_unknown_chart confirms clear error on missing chart.
func TestChartPath_rejects_unknown_chart(t *testing.T) {
	t.Parallel()

	_, err := ChartPath("nonexistent-chart-xyz")
	if err == nil {
		t.Fatal("expected error for unknown chart, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error %q should mention 'not found'", err.Error())
	}
}

// TestChartPath_rejects_empty_name confirms input validation.
func TestChartPath_rejects_empty_name(t *testing.T) {
	t.Parallel()

	_, err := ChartPath("")
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}

// TestIsLibraryChart_detects_loopzero_common confirms library chart detection
// per ADR D12 (EC-3 fix).
func TestIsLibraryChart_detects_loopzero_common(t *testing.T) {
	t.Parallel()

	chartPath, err := ChartPath("loopzero-common")
	if err != nil {
		t.Fatalf("resolve loopzero-common: %v", err)
	}

	isLib, err := IsLibraryChart(chartPath)
	if err != nil {
		t.Fatalf("IsLibraryChart: %v", err)
	}
	if !isLib {
		t.Error("loopzero-common should be detected as library chart")
	}
}

// TestIsLibraryChart_rejects_application_chart confirms loopzero-api is NOT
// classified as library (regression guard).
func TestIsLibraryChart_rejects_application_chart(t *testing.T) {
	t.Parallel()

	chartPath, err := ChartPath("loopzero-api")
	if err != nil {
		t.Fatalf("resolve loopzero-api: %v", err)
	}

	isLib, err := IsLibraryChart(chartPath)
	if err != nil {
		t.Fatalf("IsLibraryChart: %v", err)
	}
	if isLib {
		t.Error("loopzero-api should NOT be detected as library chart (it's type: application)")
	}
}

// TestNewOptions_no_env_values returns valid options with chart defaults
// only when envValuesFile is empty.
func TestNewOptions_no_env_values(t *testing.T) {
	t.Parallel()

	// Use a small chart without deps to keep test fast.
	chartPath, err := ChartPath("smoke-test")
	if err != nil {
		t.Skipf("smoke-test chart not present: %v", err)
	}

	opts, err := NewOptions(chartPath, "")
	if err != nil {
		t.Fatalf("NewOptions: %v", err)
	}
	if opts == nil {
		t.Fatal("opts should not be nil")
	}
	if len(opts.ValuesFiles) != 0 {
		t.Errorf("expected 0 values files when envValuesFile empty, got %d",
			len(opts.ValuesFiles))
	}
}

// TestNewOptions_rejects_gotmpl_values confirms D10 enforcement: helmfile
// .gotmpl files have template directives that helm.RenderTemplate cannot
// parse.
func TestNewOptions_rejects_gotmpl_values(t *testing.T) {
	t.Parallel()

	chartPath, err := ChartPath("loopzero-api")
	if err != nil {
		t.Fatalf("resolve loopzero-api: %v", err)
	}

	// Use an actual .gotmpl file from helmfile values dir
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	gotmplPath := filepath.Join(root, "infra", "helmfile", "values", "drill", "loopzero-api.yaml.gotmpl")
	if _, err := os.Stat(gotmplPath); err != nil {
		t.Skipf("gotmpl file not present: %v", err)
	}

	_, err = NewOptions(chartPath, gotmplPath)
	if err == nil {
		t.Fatal("expected error when passing .gotmpl file with template directives, got nil")
	}
	if !strings.Contains(err.Error(), "template directives") {
		t.Errorf("error should mention template directives, got: %v", err)
	}
}

// TestNewOptions_rejects_missing_env_file confirms clear error.
func TestNewOptions_rejects_missing_env_file(t *testing.T) {
	t.Parallel()

	chartPath, err := ChartPath("loopzero-api")
	if err != nil {
		t.Fatalf("resolve loopzero-api: %v", err)
	}

	_, err = NewOptions(chartPath, "/nonexistent/path/values.yaml")
	if err == nil {
		t.Fatal("expected error for missing env file, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

// TestRepoRoot_resolves_from_anywhere addresses EC-5: chart path resolution
// from non-CWD invocation. repoRoot uses runtime.Caller, not CWD, so should
// work regardless of where tests are invoked from.
func TestRepoRoot_resolves_from_anywhere(t *testing.T) {
	t.Parallel()

	// Save CWD, change to /tmp, restore at end
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		if err := os.Chdir(originalCwd); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	}()

	if err := os.Chdir("/tmp"); err != nil {
		t.Fatalf("chdir /tmp: %v", err)
	}

	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot from /tmp: %v", err)
	}
	if !strings.Contains(root, "/loopzero") {
		t.Errorf("expected root to contain /loopzero, got %s", root)
	}
}
