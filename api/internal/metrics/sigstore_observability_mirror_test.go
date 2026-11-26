// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metrics

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Defensive consistency test for the T4.1↔T4.3↔T4.2 observability
// triangle of plan 2026-05-28-meet-001-engineering-closure-plan.md.
//
// Pattern is the same as the schema-mirror test in api/internal/db/
// (T2.2): an engineering deliverable (T4.1 metrics) is mirrored by
// two downstream artifacts (T4.3 Grafana dashboard + T4.2 Prometheus
// alerts). If a future operator adds a metric without updating the
// dashboard, OR removes a metric still referenced by the dashboard,
// the test fails loud at CI time instead of producing silent
// observability drift in production.
//
// Hermetic: no PG, no live cluster. Just parses files under the repo
// root using a portable upward-walk to locate the repo from the
// test's package dir.

// metricNameRe captures Prometheus metric names of the canonical
// LoopRuntime style: loop_runtime_<subsystem>_<name>. Tests that reference the
// bucket / count / sum auto-suffixes (for histograms) are mapped
// back to the parent metric name by trimming `_bucket`, `_count`,
// `_sum`.
var metricNameRe = regexp.MustCompile(`\bloop_runtime_[a-z][a-z0-9_]*\b`)

// declaredMetricRe captures `Name: "loop_runtime_<metric>"` declarations
// from sigstore_metrics.go promauto invocations.
var declaredMetricRe = regexp.MustCompile(`Name:\s*"(loop_runtime_[a-z0-9_]+)"`)

// trimHistogramSuffix maps `loop_runtime_x_bucket` → `loop_runtime_x`. Idempotent
// on non-histogram names.
func trimHistogramSuffix(name string) string {
	for _, suf := range []string{"_bucket", "_count", "_sum"} {
		if strings.HasSuffix(name, suf) {
			return strings.TrimSuffix(name, suf)
		}
	}
	return name
}

// findRepoRoot walks upward from start looking for the repository root
// (identified by `.git` AND `go.work`).
//
// B-178: `.git` is checked for EXISTENCE, not for being a directory. It is a
// directory only in an ordinary clone. In a linked worktree — what
// `git worktree add` produces, and what every lane now works in — `.git` is a
// regular file holding a `gitdir:` pointer. Requiring os.Stat(...).IsDir()
// made the walk climb past the real root to `/` and fail, so these tests
// failed in every worktree and in no ordinary checkout. A guard that is red
// only in the environment people actually use is a guard people learn to
// ignore, which is worse than not having it.
//
// The `go.work` half is what keeps the relaxed check honest: a stray
// `.git` file somewhere above would otherwise be enough to stop the walk.
//
// Split out of repoRoot so the discovery can be tested against both shapes
// without chdir'ing the test process — see repo_root_test.go. The same
// relaxation is applied in
// cmd/loop-runtime-ops/internal/quality/quarantineprotect.RepoRootFromCwd, which is a
// different Go module and therefore cannot share this function.
func findRepoRoot(start string) (string, error) {
	dir := start
	for i := 0; i < 8; i++ {
		gitPath := filepath.Join(dir, ".git")
		workPath := filepath.Join(dir, "go.work")
		if _, err := os.Stat(gitPath); err == nil {
			if _, err := os.Stat(workPath); err == nil {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("could not locate repo root from %s", dir)
}

// repoRoot resolves the repository root from the test's working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root, err := findRepoRoot(wd)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// extractFromFile returns the unique loop_runtime_* metric names referenced
// in the file at path, with histogram suffixes trimmed. Returns
// (nil, true) when the file is missing — caller decides whether to
// treat that as a test skip or failure.
func extractFromFile(path string) (map[string]bool, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	out := map[string]bool{}
	for _, m := range metricNameRe.FindAllString(string(data), -1) {
		trimmed := trimHistogramSuffix(m)
		out[trimmed] = true
	}
	return out, true
}

// extractDeclarations parses sigstore_metrics.go and returns the set
// of metric names declared via promauto.New* with Name: "loop_runtime_*".
func extractDeclarations(t *testing.T, path string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sigstore_metrics.go: %v", err)
	}
	out := map[string]bool{}
	for _, m := range declaredMetricRe.FindAllStringSubmatch(string(data), -1) {
		out[m[1]] = true
	}
	return out
}

// TestSigstoreMetrics_AllDeclaredAreVisualized asserts every metric
// declared in sigstore_metrics.go appears in either the Grafana
// dashboard JSON (sigstore-pipeline.json) or the PrometheusRule
// alerts (prometheusrule-sigstore.yaml in the bootstrap chart).
// At least one of the two MUST reference the metric — otherwise
// the metric is registered but invisible to operators (the exact
// failure mode CR-8 was trying to prevent: "you cannot detect
// Sigstore brownouts in prod").
func TestSigstoreMetrics_AllDeclaredAreVisualized(t *testing.T) {
	root := repoRoot(t)

	// Sources of truth.
	declared := extractDeclarations(t, filepath.Join(root, "api/internal/metrics/sigstore_metrics.go"))
	dashboard, dashOK := extractFromFile(filepath.Join(root, "infra/helm/monitoring/dashboards/sigstore-pipeline.json"))
	alerts, alertOK := extractFromFile(filepath.Join(root, "infra/helm/monitoring/manifests/prometheusrule-sigstore.yaml"))

	if !dashOK {
		t.Error("T4.3 Grafana dashboard sigstore-pipeline.json missing — observability gap")
	}
	if !alertOK {
		t.Error("T4.2 PrometheusRule sigstore alerts missing — observability gap")
	}
	if !dashOK && !alertOK {
		t.Fatal("both downstream artifacts missing; cannot mirror — stop")
	}

	if len(declared) == 0 {
		t.Fatal("zero metrics extracted from sigstore_metrics.go — regex broken or file empty")
	}

	// Sorted output for stable test logs.
	declaredList := mapKeys(declared)
	sort.Strings(declaredList)

	var orphan []string
	for _, m := range declaredList {
		if !dashboard[m] && !alerts[m] {
			orphan = append(orphan, m)
		}
	}
	if len(orphan) > 0 {
		t.Errorf("T4.1 metrics with NO downstream visualization (dashboard nor alert): %v\n"+
			"Each metric MUST appear in either sigstore-pipeline.json (T4.3) or "+
			"prometheusrule-sigstore.yaml (T4.2). Add a panel or an alert.",
			orphan)
	}
}

// TestSigstoreMetrics_DashboardRefsAreDeclared asserts the reverse:
// every loop_runtime_cosign_*, loop_runtime_release_attestation_*, loop_runtime_fulcio_*
// metric referenced by the dashboard must be a real declaration in
// sigstore_metrics.go. Catches "dashboard panel pointing at a
// metric we forgot to ship" — the failure mode where the dashboard
// looks correct in a PR but renders empty panels in production.
func TestSigstoreMetrics_DashboardRefsAreDeclared(t *testing.T) {
	root := repoRoot(t)
	declared := extractDeclarations(t, filepath.Join(root, "api/internal/metrics/sigstore_metrics.go"))
	dashboard, ok := extractFromFile(filepath.Join(root, "infra/helm/monitoring/dashboards/sigstore-pipeline.json"))
	if !ok {
		t.Skip("dashboard file missing — covered by AllDeclaredAreVisualized")
	}

	var ghost []string
	for m := range dashboard {
		// Only check the T4.1 namespace; the dashboard may also
		// reference older metrics from other plans (e.g.,
		// loop_runtime_builds_total) that are declared elsewhere in
		// metrics.go. Restrict to the Sigstore namespace.
		if !strings.HasPrefix(m, "loop_runtime_cosign_") &&
			!strings.HasPrefix(m, "loop_runtime_release_attestation_") &&
			!strings.HasPrefix(m, "loop_runtime_fulcio_") {
			continue
		}
		if !declared[m] {
			ghost = append(ghost, m)
		}
	}
	if len(ghost) > 0 {
		sort.Strings(ghost)
		t.Errorf("T4.3 dashboard references metrics NOT declared in sigstore_metrics.go: %v\n"+
			"Either add the metric declaration (T4.1) or remove the dashboard reference.",
			ghost)
	}
}

func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// emissionCallRe matches `<MetricName>.WithLabelValues(...).<Op>(...)`
// for Vec metrics, or `<MetricName>.<Op>(...)` for plain metrics.
// Op is Observe/Inc/Add/Set — the four canonical Prometheus mutators.
var emissionCallRe = regexp.MustCompile(`(?m)\b([A-Z][A-Za-z0-9]+)\.(?:WithLabelValues\([^)]*\)\.)?(?:Observe|Inc|Add|Set)\(`)

// metricVarToName maps an exported Go variable name (e.g.
// CosignSignTotal) back to its declared Prometheus name (e.g.
// loop_runtime_cosign_sign_total). Used to cross-reference emission sites
// against the declared-metric set.
var metricVarToName = map[string]string{
	"CosignSignDuration":                "loop_runtime_cosign_sign_duration_seconds",
	"CosignAttestDuration":              "loop_runtime_cosign_attest_duration_seconds",
	"CosignSignTotal":                   "loop_runtime_cosign_sign_total",
	"CosignAttestTotal":                 "loop_runtime_cosign_attest_total",
	"CosignSignErrors":                  "loop_runtime_cosign_sign_errors_total",
	"CosignAttestErrors":                "loop_runtime_cosign_attest_errors_total",
	"ReleaseAttestationRekorURLPersist": "loop_runtime_release_attestation_rekor_url_null_total",
	"FulcioCertIssuanceDuration":        "loop_runtime_fulcio_cert_issuance_duration_seconds",
}

// TestSigstoreMetrics_AllDeclaredHaveWrapper asserts every metric
// declared in sigstore_metrics.go has at least one Observe/Inc/Add/Set
// emission site WITHIN the metrics package (i.e. an Observe* wrapper
// touches it). This catches the "orphan metric" class: declared and
// dashboarded but with no emission code path anywhere — guaranteed
// to render empty panels in production no matter how much chaos /
// real load the platform sees.
//
// Found this case live during the 25th Ralph iteration (commit
// 9e47759f run): `loop_runtime_fulcio_cert_issuance_duration_seconds` was
// declared + dashboarded (T4.3 Panel 32) but had zero emission code
// paths. Fixed in this commit by adding `ObserveFulcioCertIssuance`.
//
// The wrapper does NOT need to currently be CALLED from production
// code (the operator follow-up for cosign script duration outputs
// is documented inline). It just needs to exist, so future engineers
// have a definitively-named extension point to wire into when the
// dependency is ready.
func TestSigstoreMetrics_AllDeclaredHaveWrapper(t *testing.T) {
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "api/internal/metrics/sigstore_metrics.go"))
	if err != nil {
		t.Fatalf("read sigstore_metrics.go: %v", err)
	}
	srcStr := string(src)

	// Find emission sites: every <VarName>(.WithLabelValues(...))?.Op(
	emitted := map[string]bool{}
	for _, m := range emissionCallRe.FindAllStringSubmatch(srcStr, -1) {
		varName := m[1]
		if metric, ok := metricVarToName[varName]; ok {
			emitted[metric] = true
		}
	}

	// Cross-reference against declared metrics.
	declared := extractDeclarations(t, filepath.Join(root, "api/internal/metrics/sigstore_metrics.go"))
	declaredList := mapKeys(declared)
	sort.Strings(declaredList)

	var orphans []string
	for _, m := range declaredList {
		if !emitted[m] {
			orphans = append(orphans, m)
		}
	}
	if len(orphans) > 0 {
		t.Errorf("declared T4.1 metrics with NO Observe wrapper inside sigstore_metrics.go: %v\n"+
			"Add an Observe<Name>() function that calls the metric's Observe/Inc — even if "+
			"no production code calls the wrapper yet, the wrapper marks the metric as "+
			"intentionally-extension-ready, not orphan.", orphans)
	}
}
