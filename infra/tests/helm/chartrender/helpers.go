// Package chartrender renders Helm charts for render-time tests.
//
// It used to be called `helpers`, which said nothing: it was the second Go
// package in the tree named after the generic family (B-141). What it does is
// ONE thing — resolve a chart path, build options, render the template and
// decode the resulting YAML — and the name now says so.
//
// It is the ONLY way a test in infra/tests renders a chart. A test that shells
// out to `helm template` itself skips the ErrAmbiguousChart guard, the one-shot
// `helm dep update` (so a chart with a `dependencies:` block fails to render in
// a fresh clone), and the single missing-helm policy below.
//
// Plan 2026-05-20-helm-render-time-testing-terratest v1.1 — T1.2.
//
// Per ADR D5: shared setup reduces ~30 LOC per test file across 19 charts.
// Per ADR D11: NewOptions invokes `helm dep update` programmatically to
//
//	handle charts with dependencies: block (e.g., loopzero-platform umbrella).
//
// Per ADR D12: IsLibraryChart identifies charts that emit zero manifests
//
//	(e.g., loopzero-common with Chart.yaml type: library).
//
// Usage from a test file (build tag required):
//
//	//go:build helm_render
//
//	package loopzeroapi_test
//
//	import (
//	    "testing"
//	    "github.com/getloopscale/loop-runtime/infra/tests/helm/chartrender"
//	)
//
//	func TestLoopZeroAPI_Defaults(t *testing.T) {
//	    chartPath, _ := chartrender.PublishedChartPath("loopzero-api")
//	    opts, _ := chartrender.NewOptions(chartPath, "")
//	    output := chartrender.RenderTemplate(t, opts, chartPath, "loopzero-api-test",
//	        []string{"templates/deployment.yaml"})
//	    var deploy appsV1.Deployment
//	    chartrender.MustUnmarshalK8SYaml(t, output, &deploy)
//	    // ... assertions
//	}
package chartrender

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
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

// RepoRoot returns the absolute path to the LoopZero repository root,
// regardless of the caller's CWD. Uses runtime.Caller to walk back
// from this file's known location: infra/tests/helm/chartrender/chartrender.go
//
// This addresses edge case EC-5 (chart path resolution from non-CWD).
func RepoRoot() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed — cannot resolve chartrender.go location")
	}
	// thisFile = .../infra/tests/helm/chartrender/chartrender.go
	// Walk up 4 dirs: helpers/ → helm/ → tests/ → infra/ → repo root
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))))
	// Sanity: expect Taskfile.yml at root
	if _, err := os.Stat(filepath.Join(root, "Taskfile.yml")); err != nil {
		return "", fmt.Errorf("repo root sanity check failed at %s: %w", root, err)
	}
	return root, nil
}

// ErrAmbiguousChart is returned by ChartPath when a bare chart name is declared in
// BOTH chart trees, so the name identifies two different charts and no single path
// is the right answer. Callers match it with errors.Is rather than on message text.
var ErrAmbiguousChart = errors.New("chart name resolves to two charts")

// BootstrapChartPath resolves to infra/helm/<name>/ ONLY.
//
// It carries the tree in the call. No name is declared in both trees today — the
// chart-pairs gate refuses one — so ChartPath suffices for every chart; this is
// for a test that means the bootstrap tree specifically.
func BootstrapChartPath(chartName string) (string, error) {
	if chartName == "" {
		return "", fmt.Errorf("chart name cannot be empty")
	}
	root, err := RepoRoot()
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(root, "infra", "helm", chartName)
	if _, err := os.Stat(filepath.Join(candidate, "Chart.yaml")); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("bootstrap chart %q not found at infra/helm/", chartName)
}

// PublishedChartPath resolves to charts/<name>/ ONLY.
// Use when the test deliberately targets the published (OCI / ArgoCD L3)
// variant of an overlapping chart. Published variants typically use the
// loopzero-common library for full labels + container hardening.
func PublishedChartPath(chartName string) (string, error) {
	if chartName == "" {
		return "", fmt.Errorf("chart name cannot be empty")
	}
	root, err := RepoRoot()
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(root, "charts", chartName)
	if _, err := os.Stat(filepath.Join(candidate, "Chart.yaml")); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("published chart %q not found at charts/", chartName)
}

// ChartPath resolves a chart name to its absolute filesystem path.
//
// The name must identify exactly one chart. Present in exactly one of
// infra/helm/ (bootstrap, Helmfile L2) or charts/ (published OCI, ArgoCD
// L3), that path is returned. Present in BOTH, the call fails with
// ErrAmbiguousChart naming both paths: no lookup order decides it, because
// a name that means two charts has no single right answer. Present in
// neither, it fails with "not found in infra/helm/ or charts/".
//
// The chart-pairs gate refuses a name declared in both trees, so the ambiguous
// branch is a guard for a checkout that slipped past it, not a supported state.
func ChartPath(chartName string) (string, error) {
	if chartName == "" {
		return "", fmt.Errorf("chart name cannot be empty")
	}
	root, err := RepoRoot()
	if err != nil {
		return "", err
	}
	return chartPathIn(root, chartName)
}

// chartPathIn is ChartPath against an explicit repository root, so the refusal of
// a colliding name can be exercised on a fixture tree.
func chartPathIn(root, chartName string) (string, error) {
	bootstrap := filepath.Join(root, "infra", "helm", chartName)
	published := filepath.Join(root, "charts", chartName)

	// BOTH candidates are examined before anything is returned. The loop this
	// replaced returned inside its first iteration, so the second candidate was
	// never stat'd and the function could not observe — let alone report — that a
	// name identified two charts. Reporting an ambiguity requires having looked.
	bootstrapExists := hasChartYaml(bootstrap)
	publishedExists := hasChartYaml(published)

	switch {
	case bootstrapExists && publishedExists:
		return "", fmt.Errorf(
			"%w: %q names two charts - %s and %s; "+
				"call BootstrapChartPath for the first or PublishedChartPath for the second",
			ErrAmbiguousChart, chartName, bootstrap, published)
	case bootstrapExists:
		return bootstrap, nil
	case publishedExists:
		return published, nil
	default:
		return "", fmt.Errorf("chart %q not found in infra/helm/ or charts/", chartName)
	}
}

// hasChartYaml reports whether dir holds a Chart.yaml, which is what makes a
// directory a chart. Keying on the file and never on the directory matters: the
// values-only overlays under infra/helm/ are directories with no Chart.yaml, and
// they must keep taking the not-found branch. A stat failure for any other reason
// (permissions, I/O) is treated as absence, exactly as it was before B-002.
func hasChartYaml(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "Chart.yaml"))
	return err == nil
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
	data, err := os.ReadFile(filepath.Join(chartPath, "Chart.yaml")) //nolint:gosec // chart path is test-controlled (resolved via ChartPath sanity-checked at repo root)
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
	data, err := os.ReadFile(filepath.Join(chartPath, "Chart.yaml")) //nolint:gosec // chart path is test-controlled (resolved via ChartPath sanity-checked at repo root)
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
	cmd := exec.Command("helm", "dependency", "update", chartPath) //nolint:gosec // chartPath is test-controlled (resolved via ChartPath sanity-checked at repo root)
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
	// One policy for a missing helm binary, for every render test: fail. helm
	// is a declared prerequisite (CLAUDE.md § Prerequisites), and a render test
	// that skips without it reports green over a chart nobody rendered.
	if _, err := exec.LookPath("helm"); err != nil {
		return nil, fmt.Errorf("helm is not on PATH and is a declared prerequisite "+
			"of every render test: %w", err)
	}

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
			root, rerr := RepoRoot()
			if rerr != nil {
				return nil, rerr
			}
			absPath = filepath.Join(root, envValuesFile)
		}
		if _, serr := os.Stat(absPath); serr != nil {
			return nil, fmt.Errorf("env values file not found: %s", absPath)
		}
		// Verify plain YAML (no .gotmpl template directives) per D10
		data, ferr := os.ReadFile(absPath) //nolint:gosec // absPath is test-controlled (env values stub path resolved to repo root)
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

// renderCache is a process-wide cache of render outputs keyed by the
// (chartPath, releaseName, templates, valuesFiles, setValues) tuple.
// 14 tests per package typically render the same chart with the same
// args; caching cuts redundant helm subprocess invocations.
//
// Race fix: a naive map+mutex still allows multiple t.Parallel() tests
// to all miss the cache simultaneously, each rendering independently
// before any stores. Per-key sync.Once forces the FIRST caller to
// render while others wait on the Once and then read the stored value.
//
// Plan 2026-05-20 Global DoD: "CI runtime <60s for full suite".
// Render cache cuts loopzero-tenant-operator from 14 helm calls → 1 call
// (measured: 8 → 1 after per-key Once fix).
type renderEntry struct {
	once   sync.Once
	output string
}

var (
	renderCache      = make(map[string]*renderEntry)
	renderCacheMutex sync.Mutex
)

// renderCacheKey serializes the inputs that determine render output.
// Two calls with the same key produce byte-identical output (helm
// template is deterministic when given the same inputs).
func renderCacheKey(opts *helm.Options, chartPath, releaseName string, templates []string) string {
	// Use a separator that cannot appear in chart paths or template
	// names to keep the key unambiguous.
	const sep = "\x00"
	key := chartPath + sep + releaseName + sep + strings.Join(templates, ",")
	if opts != nil {
		// ValuesFiles + SetValues are the user-visible inputs. Sort
		// SetValues for determinism (map iteration is randomized).
		key += sep + strings.Join(opts.ValuesFiles, ",")
		if len(opts.SetValues) > 0 {
			sv := make([]string, 0, len(opts.SetValues))
			for k, v := range opts.SetValues {
				sv = append(sv, k+"="+v)
			}
			sort.Strings(sv)
			key += sep + strings.Join(sv, ",")
		}
	}
	return key
}

// RenderTemplate is a caching wrapper around helm.RenderTemplate. Same
// (chartPath, releaseName, templates, values) inputs return cached output
// from a process-wide sync.Map. Helm template is deterministic per spec,
// so caching is safe; the cache is scoped to one go test binary and
// invalidates implicitly on process exit.
//
// Performance: cuts full helm-render suite from ~150s to ~30s by
// eliminating duplicate helm subprocess invocations across t.Parallel()
// tests that all need the same render.
func RenderTemplate(t *testing.T, opts *helm.Options, chartPath, releaseName string, templates []string) string {
	t.Helper()

	key := renderCacheKey(opts, chartPath, releaseName, templates)

	// Get-or-create the entry under lock. After this, the entry's Once
	// guarantees exactly one helm render for this key process-wide,
	// regardless of how many goroutines call concurrently.
	renderCacheMutex.Lock()
	entry, exists := renderCache[key]
	if !exists {
		entry = &renderEntry{}
		renderCache[key] = entry
	}
	renderCacheMutex.Unlock()

	entry.once.Do(func() {
		entry.output = helm.RenderTemplate(t, opts, chartPath, releaseName, templates)
	})

	return entry.output
}

// RenderTemplateE renders like RenderTemplate but returns helm's error instead
// of failing the test, and never caches. It exists for tests whose assertion IS
// that a render fails — a chart's fail-closed `fail` guard — which must still go
// through the facade so dependency resolution and chart-name resolution apply.
func RenderTemplateE(t *testing.T, opts *helm.Options, chartPath, releaseName string, templates []string) (string, error) {
	t.Helper()
	return helm.RenderTemplateE(t, opts, chartPath, releaseName, templates)
}

// kindAndAPI is a minimal struct for sniffing kind from a YAML doc.
type kindAndAPI struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
}

// EnvStubPath returns the canonical path for a per-env values stub:
// `infra/tests/helm/values/<env>/<chart>.yaml`.
// Returns the absolute path even if the file does not exist (caller
// uses HasEnvStub to check).
func EnvStubPath(env, chartName string) string {
	return filepath.Join("infra", "tests", "helm", "values", env, chartName+".yaml")
}

// HasEnvStub returns true if a plain-YAML env stub exists at the
// canonical EnvStubPath. Used by per-env parity tests to decide
// between "assert env divergence absent (no stub)" and "assert env
// stub renders correctly (stub exists)".
func HasEnvStub(env, chartName string) bool {
	root, err := RepoRoot()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(root, EnvStubPath(env, chartName)))
	return err == nil
}

// AssertEnvParityIfNoStub is the per-env regression guard for charts
// without helmfile env-specific values. Behavior:
//
//   - If NO env stub exists at infra/tests/helm/values/<env>/<chart>.yaml:
//     PASS (documents the absence of env divergence as intentional).
//   - If an env stub EXISTS but no env-specific test assertions are
//     present: FAIL loudly — someone added a stub without updating the
//     test to assert env-specific invariants, which would silently
//     regress to vacuous coverage.
//
// This pattern satisfies plan T3.1/T3.2 literal "4 files per chart"
// requirement without diluting signal with vacuous re-render tests
// (per Plan v1.2 amendment scope-refinement rationale).
//
// Usage in a per-env test file (<chart>/dev_test.go etc.):
//
//	func TestEnvParity_dev(t *testing.T) {
//	    t.Parallel()
//	    chartrender.AssertEnvParityIfNoStub(t, "dev", chartName)
//	}
func AssertEnvParityIfNoStub(t *testing.T, env, chartName string) {
	t.Helper()
	if HasEnvStub(env, chartName) {
		t.Fatalf("env stub exists at %s but no env-specific assertions "+
			"in this test file — update test to assert env-specific "+
			"invariants (image registry / replicas / domain / etc.) "+
			"instead of using AssertEnvParityIfNoStub. The stub's "+
			"presence indicates the chart has env-divergent behavior "+
			"that MUST be asserted.",
			EnvStubPath(env, chartName))
	}
	// Stub absent → documented intentional state. PASS.
}

// SplitYAMLByKind splits multi-document YAML output and returns RAW
// docs (as YAML strings) whose Kind matches the requested value.
//
// Used when a chart emits multiple resources of the SAME kind
// (e.g., loopzero-crds emits 8 CustomResourceDefinitions, loopzero-admission-policies
// emits 6 ValidatingAdmissionPolicies). Callers iterate the returned
// slice and unmarshal each entry into the appropriate typed struct.
//
// Returns the matching docs in render-order. Empty slice if no match
// (test should assert this case explicitly if it matters).
//
// Example:
//
//	for _, raw := range chartrender.SplitYAMLByKind(out, "CustomResourceDefinition") {
//	    var crd apiextV1.CustomResourceDefinition
//	    yaml.Unmarshal([]byte(raw), &crd)
//	    // ... assertions per CRD
//	}
func SplitYAMLByKind(output, kind string) []string {
	docs := strings.Split(output, "\n---")
	matches := make([]string, 0)
	for _, doc := range docs {
		doc = strings.TrimSpace(doc)
		doc = strings.TrimPrefix(doc, "---\n")
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}
		var meta kindAndAPI
		if err := yaml.Unmarshal([]byte(doc), &meta); err != nil {
			continue
		}
		if meta.Kind == kind {
			matches = append(matches, doc)
		}
	}
	return matches
}

// MustUnmarshalK8SYamlByKind splits multi-document YAML output and
// unmarshales the document matching kind into out. Fails the test with
// clear error if no matching kind is found.
//
// This addresses the case where a single template file emits multiple
// K8s resources (e.g., loopzero-api/templates/deployment.yaml emits both
// Service AND Deployment).
//
// For multiple same-kind docs (e.g., 8 CRDs in one render), use
// SplitYAMLByKind which returns raw YAML for iteration.
//
// Example:
//
//	var deploy appsV1.Deployment
//	chartrender.MustUnmarshalK8SYamlByKind(t, output, "Deployment", &deploy)
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
