// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metrics

import (
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
// LoopZero style: loopzero_<subsystem>_<name>. Tests that reference the
// bucket / count / sum auto-suffixes (for histograms) are mapped
// back to the parent metric name by trimming `_bucket`, `_count`,
// `_sum`.
var metricNameRe = regexp.MustCompile(`\bloopzero_[a-z][a-z0-9_]*\b`)

// declaredMetricRe captures `Name: "loopzero_<metric>"` declarations
// from sigstore_metrics.go promauto invocations.
var declaredMetricRe = regexp.MustCompile(`Name:\s*"(loopzero_[a-z0-9_]+)"`)

// trimHistogramSuffix maps `loopzero_x_bucket` → `loopzero_x`. Idempotent
// on non-histogram names.
func trimHistogramSuffix(name string) string {
	for _, suf := range []string{"_bucket", "_count", "_sum"} {
		if strings.HasSuffix(name, suf) {
			return strings.TrimSuffix(name, suf)
		}
	}
	return name
}

// repoRoot walks upward from the test file's directory looking for
// the repository root (identified by `.git` AND `CHANGELOG.md`).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		gitDir := filepath.Join(dir, ".git")
		changelogPath := filepath.Join(dir, "CHANGELOG.md")
		if st, err := os.Stat(gitDir); err == nil && st.IsDir() {
			if _, err := os.Stat(changelogPath); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate repo root from %s", dir)
	return ""
}

// extractFromFile returns the unique loopzero_* metric names referenced
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
// of metric names declared via promauto.New* with Name: "loopzero_*".
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
// every loopzero_cosign_*, loopzero_release_attestation_*, loopzero_fulcio_*
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
		// loopzero_builds_total) that are declared elsewhere in
		// metrics.go. Restrict to the Sigstore namespace.
		if !strings.HasPrefix(m, "loopzero_cosign_") &&
			!strings.HasPrefix(m, "loopzero_release_attestation_") &&
			!strings.HasPrefix(m, "loopzero_fulcio_") {
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
