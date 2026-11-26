//go:build helm_render

// Package helpers self-tests.
//
// Plan 2026-05-20-helm-render-time-testing-terratest v1.1 — T1.2 TDD.
//
// Per skill rules: tests written FIRST (RED), implementation later (GREEN).
// All tests in this file validate the helpers package contract.
package chartrender

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestChartPath_resolves_known_charts ensures both infra/helm/ and
// charts/ lookups work.
func TestChartPath_resolves_known_charts(t *testing.T) {
	t.Parallel()

	// Every name below exists in exactly ONE tree, which is the only case
	// ChartPath resolves. A name declared in BOTH is refused, not preferred —
	// see TestChartPath_refuses_a_colliding_name.
	// postgresql + cert-manager are upstream-wrappers (no Chart.yaml) —
	// they would be auto-exempt per coverage gate, not testable here.
	cases := []struct {
		name      string
		chartName string
		wantDir   string // expected to contain this segment
	}{
		{"chaos-mesh lives in infra/helm/ with Chart.yaml", "chaos-mesh", "infra/helm/chaos-mesh"},
		{"loopruntime-app-operator lives in charts/", "loopruntime-app-operator", "charts/loopruntime-app-operator"},
		{"loopruntime-common (library) lives in charts/", "loopruntime-common", "charts/loopruntime-common"},
		{"loopruntime-cloud lives in charts/ only since its bootstrap copy was deleted", "loopruntime-cloud", "charts/loopruntime-cloud"},
		{"loopruntime-api lives in charts/ only since its bootstrap copy was deleted", "loopruntime-api", "charts/loopruntime-api"},
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

// TestIsLibraryChart_detects_loopruntime_common confirms library chart detection
// per ADR D12 (EC-3 fix).
// TestChartPath_refuses_a_colliding_name asserts that a name declared in BOTH
// chart trees resolves to no chart at all.
//
// Before B-002, ChartPath returned infra/helm/<name> for a colliding name because
// that candidate sat at index 0 of the candidate slice — the identity of the chart
// was decided by slice order, and the published chart was unreachable through this
// function. The error must do more than fail: it names both paths and both explicit
// resolvers, so the caller is told what to call instead.
//
// No name collides in the repository any more (the last pair, loopruntime-api, became one
// chart), so the collision is built in a fixture tree.
func TestChartPath_refuses_a_colliding_name(t *testing.T) {
	t.Parallel()

	const name = "colliding-chart"
	root := t.TempDir()
	for _, tree := range []string{filepath.Join("infra", "helm"), "charts"} {
		dir := filepath.Join(root, tree, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte("name: "+name+"\n"), 0o600); err != nil {
			t.Fatalf("write Chart.yaml: %v", err)
		}
	}

	got, err := chartPathIn(root, name)
	if !errors.Is(err, ErrAmbiguousChart) {
		t.Fatalf("chartPathIn(%q) err = %v; want ErrAmbiguousChart", name, err)
	}
	if got != "" {
		t.Errorf("chartPathIn(%q) = %q; want empty path on refusal", name, got)
	}
	for _, want := range []string{
		filepath.Join("infra", "helm", name),
		filepath.Join("charts", name),
		"BootstrapChartPath",
		"PublishedChartPath",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("chartPathIn(%q) error %q does not name %q", name, err, want)
		}
	}
}

func TestIsLibraryChart_detects_loopruntime_common(t *testing.T) {
	t.Parallel()

	chartPath, err := ChartPath("loopruntime-common")
	if err != nil {
		t.Fatalf("resolve loopruntime-common: %v", err)
	}

	isLib, err := IsLibraryChart(chartPath)
	if err != nil {
		t.Fatalf("IsLibraryChart: %v", err)
	}
	if !isLib {
		t.Error("loopruntime-common should be detected as library chart")
	}
}

// TestIsLibraryChart_rejects_application_chart confirms loopruntime-api is NOT
// classified as library (regression guard).
func TestIsLibraryChart_rejects_application_chart(t *testing.T) {
	t.Parallel()

	chartPath, err := PublishedChartPath("loopruntime-api")
	if err != nil {
		t.Fatalf("resolve loopruntime-api: %v", err)
	}

	isLib, err := IsLibraryChart(chartPath)
	if err != nil {
		t.Fatalf("IsLibraryChart: %v", err)
	}
	if isLib {
		t.Error("loopruntime-api should NOT be detected as library chart (it's type: application)")
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

	chartPath, err := PublishedChartPath("loopruntime-api")
	if err != nil {
		t.Fatalf("resolve loopruntime-api: %v", err)
	}

	// Use an actual .gotmpl file from helmfile values dir
	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	gotmplPath := filepath.Join(root, "infra", "helmfile", "values", "drill", "loopruntime-api.yaml.gotmpl")
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

	chartPath, err := PublishedChartPath("loopruntime-api")
	if err != nil {
		t.Fatalf("resolve loopruntime-api: %v", err)
	}

	_, err = NewOptions(chartPath, "/nonexistent/path/values.yaml")
	if err == nil {
		t.Fatal("expected error for missing env file, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

// TestBootstrapChartPath_resolves_a_bootstrap_chart confirms explicit
// bootstrap-only resolution. chaos-mesh exists ONLY in infra/helm/.
func TestBootstrapChartPath_resolves_a_bootstrap_chart(t *testing.T) {
	t.Parallel()

	got, err := BootstrapChartPath("chaos-mesh")
	if err != nil {
		t.Fatalf("BootstrapChartPath(chaos-mesh): %v", err)
	}
	if !strings.Contains(got, filepath.Join("infra", "helm", "chaos-mesh")) {
		t.Errorf("BootstrapChartPath(chaos-mesh)=%q must contain infra/helm/chaos-mesh", got)
	}
}

// TestPublishedChartPath_resolves_overlapping_charts confirms explicit
// published-variant resolution for the charts that had a bootstrap copy in
// infra/helm/ (loopruntime-api, loopruntime-cloud) until each pair became one chart.
func TestPublishedChartPath_resolves_overlapping_charts(t *testing.T) {
	t.Parallel()

	cases := []string{"loopruntime-api", "loopruntime-cloud"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := PublishedChartPath(name)
			if err != nil {
				t.Fatalf("PublishedChartPath(%q): %v", name, err)
			}
			if !strings.Contains(got, filepath.Join("charts", name)) {
				t.Errorf("PublishedChartPath(%q)=%q must contain charts/%s",
					name, got, name)
			}
		})
	}
}

// TestBootstrapChartPath_rejects_published_only_chart confirms that
// asking for the bootstrap variant of a published-only chart errors
// clearly (no fallback). loopruntime-dashboard exists ONLY in charts/.
func TestBootstrapChartPath_rejects_published_only_chart(t *testing.T) {
	t.Parallel()

	_, err := BootstrapChartPath("loopruntime-dashboard")
	if err == nil {
		t.Fatal("BootstrapChartPath(loopruntime-dashboard) should error — chart " +
			"exists only in charts/, no bootstrap variant")
	}
	if !strings.Contains(err.Error(), "infra/helm") {
		t.Errorf("error %q should mention infra/helm", err.Error())
	}
}

// TestPublishedChartPath_rejects_bootstrap_only_chart confirms that
// asking for the published variant of a bootstrap-only chart errors
// clearly (no fallback). cloudflared exists ONLY in infra/helm/.
func TestPublishedChartPath_rejects_bootstrap_only_chart(t *testing.T) {
	t.Parallel()

	_, err := PublishedChartPath("cloudflared")
	if err == nil {
		t.Fatal("PublishedChartPath(cloudflared) should error — chart " +
			"exists only in infra/helm/, no published variant")
	}
	if !strings.Contains(err.Error(), "charts/") {
		t.Errorf("error %q should mention charts/", err.Error())
	}
}

// TestRepoRoot_resolves_from_anywhere addresses EC-5: chart path resolution
// from non-CWD invocation. RepoRoot uses runtime.Caller, not CWD, so should
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

	root, err := RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot from /tmp: %v", err)
	}
	if !strings.Contains(root, "/loopruntime") {
		t.Errorf("expected root to contain /loopruntime, got %s", root)
	}
}
