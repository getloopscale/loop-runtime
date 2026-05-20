// Package helpers provides shared utilities for Helm chart render-time tests.
//
// Plan 2026-05-20-helm-render-time-testing-terratest v1.1 — T1.2.
//
// Per ADR D5: shared setup reduces ~30 LOC per test file across 19 charts.
// Per ADR D11: NewOptions invokes `helm dep update` programmatically to
//   handle charts with dependencies: block (e.g., loopzero-platform umbrella).
// Per ADR D12: IsLibraryChart identifies charts that emit zero manifests
//   (e.g., loopzero-common with Chart.yaml type: library).
//
// Usage from a test file (build tag required):
//
//	//go:build helm_render
//
//	package loopzeroapi_test
//
//	import (
//	    "testing"
//	    "github.com/loopzero/loopzero/infra/tests/helm/helpers"
//	)
//
//	func TestLoopZeroAPI_Defaults(t *testing.T) {
//	    chartPath, _ := helpers.ChartPath("loopzero-api")
//	    opts, _ := helpers.NewOptions(chartPath, "")
//	    output := helpers.RenderTemplate(t, opts, chartPath, "loopzero-api-test",
//	        []string{"templates/deployment.yaml"})
//	    var deploy appsV1.Deployment
//	    helpers.MustUnmarshalK8SYaml(t, output, &deploy)
//	    // ... assertions
//	}
package helpers

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/gruntwork-io/terratest/modules/helm"
	"gopkg.in/yaml.v3"
)

// depUpdateOnce caches `helm dep update` results per chart path so 13
// parallel tests in the same package (typical Microsoft 2026 category
// fan-out) do not all race for helm's lock file. Without this cache,
// concurrent invocations on a chart with a `dependencies:` block (e.g.
// loopzero-cloud → loopzero-common library) OOM-killed the test binary at ~13
// parallel goroutines (observed 2026-05-20: SIGKILL after 11min hang).
var (
	depUpdateOnce  = make(map[string]*sync.Once)
	depUpdateErr   = make(map[string]error)
	depUpdateMutex sync.Mutex
)

// resolveDeps runs helm dep update exactly once per chartPath process-wide,
// regardless of how many tests call it. Returns cached error for repeat
// calls (success: nil; failure: original error).
func resolveDeps(chartPath string) error {
	depUpdateMutex.Lock()
	once, exists := depUpdateOnce[chartPath]
	if !exists {
		once = &sync.Once{}
		depUpdateOnce[chartPath] = once
	}
	depUpdateMutex.Unlock()

	once.Do(func() {
		hasDeps, err := hasDependencies(chartPath)
		if err != nil {
			depUpdateMutex.Lock()
			depUpdateErr[chartPath] = fmt.Errorf("check dependencies: %w", err)
			depUpdateMutex.Unlock()
			return
		}
		if !hasDeps {
			return
		}
		if err := runHelmDepUpdate(chartPath); err != nil {
			depUpdateMutex.Lock()
			depUpdateErr[chartPath] = err
			depUpdateMutex.Unlock()
		}
	})

	depUpdateMutex.Lock()
	defer depUpdateMutex.Unlock()
	return depUpdateErr[chartPath]
}

// repoRoot returns the absolute path to the LoopZero repository root,
// regardless of the caller's CWD. Uses runtime.Caller to walk back
// from this file's known location: infra/tests/helm/helpers/helpers.go
//
// This addresses edge case EC-5 (chart path resolution from non-CWD).
func repoRoot() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed — cannot resolve helpers.go location")
	}
	// thisFile = .../infra/tests/helm/helpers/helpers.go
	// Walk up 4 dirs: helpers/ → helm/ → tests/ → infra/ → repo root
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))))
	// Sanity: expect Taskfile.yml at root
	if _, err := os.Stat(filepath.Join(root, "Taskfile.yml")); err != nil {
		return "", fmt.Errorf("repo root sanity check failed at %s: %w", root, err)
	}
	return root, nil
}

// ChartPath resolves a chart name to its absolute filesystem path.
//
// Lookup order (matches CLAUDE.md Helm Chart Dual System):
//  1. infra/helm/<name>/ (bootstrap charts, Helmfile L2)
//  2. charts/<name>/ (published OCI charts, ArgoCD L3)
//
// Returns error if chart not found in either location.
func ChartPath(chartName string) (string, error) {
	if chartName == "" {
		return "", fmt.Errorf("chart name cannot be empty")
	}
	root, err := repoRoot()
	if err != nil {
		return "", err
	}
	candidates := []string{
		filepath.Join(root, "infra", "helm", chartName),
		filepath.Join(root, "charts", chartName),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(filepath.Join(candidate, "Chart.yaml")); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("chart %q not found in infra/helm/ or charts/", chartName)
}

// chartYaml is the minimal subset of Chart.yaml we care about for D12
// (library chart detection).
type chartYaml struct {
	Type string `yaml:"type"`
}

// IsLibraryChart returns true if the chart at chartPath has Chart.yaml
// with `type: library`. Library charts emit zero manifests; render-time
// tests are NOT applicable per ADR D12.
//
// This addresses edge case EC-3 (loopzero-common library chart).
func IsLibraryChart(chartPath string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(chartPath, "Chart.yaml"))
	if err != nil {
		return false, fmt.Errorf("read Chart.yaml: %w", err)
	}
	var c chartYaml
	if err := yaml.Unmarshal(data, &c); err != nil {
		return false, fmt.Errorf("parse Chart.yaml: %w", err)
	}
	return c.Type == "library", nil
}

// hasDependencies returns true if the chart at chartPath has a
// `dependencies:` block in Chart.yaml. Such charts need `helm dep
// update` before render to populate the local charts/ subdirectory.
func hasDependencies(chartPath string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(chartPath, "Chart.yaml"))
	if err != nil {
		return false, fmt.Errorf("read Chart.yaml: %w", err)
	}
	// Simple textual check (avoids parsing entire Chart.yaml schema).
	// "dependencies:" at start of line + at least one item below.
	return strings.Contains(string(data), "\ndependencies:\n") ||
		strings.HasPrefix(string(data), "dependencies:\n"), nil
}

// runHelmDepUpdate invokes `helm dep update <chartPath>` to populate
// the chart's charts/ subdirectory with its declared dependencies.
//
// Idempotent: re-running is no-op if deps are already current.
//
// This addresses edge case EC-2 (charts with dependencies: block).
func runHelmDepUpdate(chartPath string) error {
	cmd := exec.Command("helm", "dependency", "update", chartPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("helm dep update %s failed: %w\noutput: %s",
			chartPath, err, output)
	}
	return nil
}

// NewOptions builds a Terratest helm.Options struct for the given chart.
// envValuesFile is the OPTIONAL path (absolute or relative to repo root)
// to a plain-YAML env-override values file. If empty, only chart defaults
// (values.yaml) are used.
//
// Per ADR D10: envValuesFile must be plain YAML (not .gotmpl) — helmfile-
// templated values are NOT directly consumable by helm.RenderTemplate.
// Test stubs live at infra/tests/helm/values/<env>/<chart>.yaml.
//
// Per ADR D11: automatically invokes `helm dep update` for charts with
// dependencies: block.
func NewOptions(chartPath, envValuesFile string) (*helm.Options, error) {
	// Resolve dependencies if chart has them (D11).
	// Uses sync.Once cache (resolveDeps) so parallel tests on the same
	// chart do not all race for helm's lock file → OOM.
	if err := resolveDeps(chartPath); err != nil {
		return nil, err
	}

	opts := &helm.Options{}

	if envValuesFile != "" {
		// Resolve to absolute if relative
		absPath := envValuesFile
		if !filepath.IsAbs(envValuesFile) {
			root, rerr := repoRoot()
			if rerr != nil {
				return nil, rerr
			}
			absPath = filepath.Join(root, envValuesFile)
		}
		if _, serr := os.Stat(absPath); serr != nil {
			return nil, fmt.Errorf("env values file not found: %s", absPath)
		}
		// Verify plain YAML (no .gotmpl template directives) per D10
		data, ferr := os.ReadFile(absPath)
		if ferr != nil {
			return nil, fmt.Errorf("read env values: %w", ferr)
		}
		if strings.Contains(string(data), "{{") {
			return nil, fmt.Errorf(
				"env values file %s contains helmfile template directives ('{{'); "+
					"create plain-YAML stub at infra/tests/helm/values/<env>/<chart>.yaml per D10",
				absPath)
		}
		opts.ValuesFiles = []string{absPath}
	}

	return opts, nil
}

// RenderTemplate is a thin wrapper around helm.RenderTemplate. Provided
// for symmetry with the rest of the helpers package — direct usage of
// helm.RenderTemplate is also fine.
func RenderTemplate(t *testing.T, opts *helm.Options, chartPath, releaseName string, templates []string) string {
	t.Helper()
	return helm.RenderTemplate(t, opts, chartPath, releaseName, templates)
}

// MustUnmarshalK8SYaml unmarshales rendered YAML output into the given
// K8s API struct. Fails the test with clear error on parse failure.
//
// WARNING: helm.UnmarshalK8SYaml parses only the FIRST YAML document.
// Use MustUnmarshalK8SYamlByKind for templates that emit multiple kinds
// (e.g., loopzero-api/templates/deployment.yaml emits Service + Deployment).
//
// Use typed K8s structs from k8s.io/api/<group>/<version> (e.g.,
// appsV1.Deployment, coreV1.Service) for compile-time field path
// validation per the Microsoft 2026 type-safety argument.
func MustUnmarshalK8SYaml(t *testing.T, output string, out interface{}) {
	t.Helper()
	helm.UnmarshalK8SYaml(t, output, out)
}

// kindAndAPI is a minimal struct for sniffing kind from a YAML doc.
type kindAndAPI struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
}

// MustUnmarshalK8SYamlByKind splits multi-document YAML output and
// unmarshales the document matching kind into out. Fails the test with
// clear error if no matching kind is found.
//
// This addresses the case where a single template file emits multiple
// K8s resources (e.g., loopzero-api/templates/deployment.yaml emits both
// Service AND Deployment).
//
// Example:
//
//	var deploy appsV1.Deployment
//	helpers.MustUnmarshalK8SYamlByKind(t, output, "Deployment", &deploy)
func MustUnmarshalK8SYamlByKind(t *testing.T, output, kind string, out interface{}) {
	t.Helper()
	// Split by document separator (--- at start of line)
	docs := strings.Split(output, "\n---")
	for _, doc := range docs {
		// Trim leading whitespace/separator artifacts
		doc = strings.TrimSpace(doc)
		// Strip leading --- if present (first doc may not have it)
		doc = strings.TrimPrefix(doc, "---\n")
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}
		var meta kindAndAPI
		if err := yaml.Unmarshal([]byte(doc), &meta); err != nil {
			// Not parseable as K8s resource; skip
			continue
		}
		if meta.Kind == kind {
			helm.UnmarshalK8SYaml(t, doc, out)
			return
		}
	}
	t.Fatalf("MustUnmarshalK8SYamlByKind: no document with kind=%q found in render output (%d docs scanned)",
		kind, len(docs))
}
