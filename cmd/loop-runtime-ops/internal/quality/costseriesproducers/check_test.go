// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package costseriesproducers

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- fixtures ---------------------------------------------------------------

// fakeRepo builds a synthetic repo root. files is repo-relative path -> body.
func fakeRepo(t *testing.T, decl string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, DefaultDeclarationFile), decl)
	for rel, body := range files {
		write(t, filepath.Join(root, rel), body)
	}
	return root
}

// fakeSibling builds a synthetic loop-console with the given aggregator body.
func fakeSibling(t *testing.T, aggregator string) string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, DefaultAggregatorFile), aggregator)
	return root
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// absentSibling is a path that certainly does not exist, so the drift half is
// deliberately unavailable.
func absentSibling(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "no-loop-console-here")
}

const aggregatorWithThree = `package billing

var promQueries = map[planpkg.CostMetric]string{
	planpkg.MetricEgressBytes:  ` + "`sum by (tenant) (loop_runtime_egress_bytes_total)`" + `,
	planpkg.MetricDOCRPulls:    ` + "`sum by (tenant) (loop_runtime_docr_pulls_total)`" + `,
	planpkg.MetricBuildMinutes: ` + "`sum by (tenant) (rate(loop_runtime_build_duration_seconds_sum[5m]) * 60)`" + `,
}
`

const metricsGo = `package metrics

var BuildDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "loop_runtime_build_duration_seconds",
	Help:    "Build duration.",
}, []string{"tenant"})
`

const threeLineDecl = `# comment
loop_runtime_egress_bytes_total          ABSENT B-181
loop_runtime_docr_pulls_total            ABSENT B-181
loop_runtime_build_duration_seconds_sum  PRODUCER api/internal/metrics/metrics.go
`

func mustCheck(t *testing.T, cfg Config) *Outcome {
	t.Helper()
	out, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: unexpected setup error: %v", err)
	}
	return out
}

func joined(v []string) string { return strings.Join(v, "\n") }

// --- 1. the series is emitted here -----------------------------------------

func TestProducerDeclaredAndVerified(t *testing.T) {
	root := fakeRepo(t, threeLineDecl, map[string]string{
		"api/internal/metrics/metrics.go": metricsGo,
	})
	out := mustCheck(t, Config{RepoRoot: root, SiblingRoot: absentSibling(t)})

	if !out.Pass {
		t.Fatalf("producer contract should hold; violations:\n%s", joined(out.Violations))
	}
	if len(out.VerifiedProducers) != 1 ||
		!strings.Contains(out.VerifiedProducers[0], "loop_runtime_build_duration_seconds_sum") {
		t.Errorf("expected the _sum series verified against its producer, got %v", out.VerifiedProducers)
	}
}

func TestProducerPathThatDoesNotExistFails(t *testing.T) {
	root := fakeRepo(t, threeLineDecl, nil) // metrics.go deliberately absent
	out := mustCheck(t, Config{RepoRoot: root, SiblingRoot: absentSibling(t)})

	if out.Pass {
		t.Fatal("a declaration naming a producer file that is not in the tree must FAIL")
	}
	if !strings.Contains(joined(out.Violations), "cannot be read") {
		t.Errorf("the violation must say the producer file could not be read, got:\n%s", joined(out.Violations))
	}
}

// A producer column that is never opened is prose. This is the assertion that
// stops the file from decaying into a comment.
func TestProducerFileThatDoesNotDeclareTheMetricFails(t *testing.T) {
	root := fakeRepo(t, threeLineDecl, map[string]string{
		"api/internal/metrics/metrics.go": `package metrics

var Other = promauto.NewCounter(prometheus.CounterOpts{Name: "loop_runtime_something_else_total"})
`,
	})
	out := mustCheck(t, Config{RepoRoot: root, SiblingRoot: absentSibling(t)})

	if out.Pass {
		t.Fatal("a producer file that declares a DIFFERENT metric must FAIL")
	}
	if !strings.Contains(joined(out.Violations), "declares no `Name:") {
		t.Errorf("the violation must name the missing declaration, got:\n%s", joined(out.Violations))
	}
}

func TestHistogramDerivedSeriesResolvesToItsBaseMetric(t *testing.T) {
	if got := producerKey("loop_runtime_build_duration_seconds_sum"); got != "loop_runtime_build_duration_seconds" {
		t.Errorf("_sum must resolve to the base metric, got %q", got)
	}
	for _, s := range []string{"loop_runtime_x_count", "loop_runtime_x_bucket"} {
		if got := producerKey(s); got != "loop_runtime_x" {
			t.Errorf("producerKey(%q) = %q, want loop_runtime_x", s, got)
		}
	}
}

// _total is part of a counter's DECLARED name here (loop_runtime_builds_total), so
// stripping it would look up a metric nobody declares and report a producer as
// missing.
func TestTotalSuffixIsNotStripped(t *testing.T) {
	if got := producerKey("loop_runtime_builds_total"); got != "loop_runtime_builds_total" {
		t.Errorf("_total must survive: producerKey = %q", got)
	}
}

// --- 2. the series is emitted by nobody ------------------------------------

func TestTrackedAbsenceIsVisibleAndNotAFailure(t *testing.T) {
	root := fakeRepo(t, threeLineDecl, map[string]string{
		"api/internal/metrics/metrics.go": metricsGo,
	})
	out := mustCheck(t, Config{RepoRoot: root, SiblingRoot: absentSibling(t)})

	if len(out.TrackedAbsences) != 2 {
		t.Fatalf("expected the two undeclared series reported as tracked absences, got %v", out.TrackedAbsences)
	}
	if !strings.Contains(joined(out.TrackedAbsences), "B-181") {
		t.Errorf("a tracked absence must carry its work item, got %v", out.TrackedAbsences)
	}
	if !out.Pass {
		t.Errorf("a TRACKED absence must not turn the gate red -- that is the permanently-red trap; violations:\n%s",
			joined(out.Violations))
	}
}

// The ledger must not outlive the gap. This is the mirror of
// knownConsumerGaps in api/internal/contract/cli_consumer_test.go.
func TestAbsenceThatIsNoLongerAbsentFails(t *testing.T) {
	root := fakeRepo(t, threeLineDecl, map[string]string{
		"api/internal/metrics/metrics.go": metricsGo,
		"api/internal/metrics/egress.go": `package metrics

var Egress = promauto.NewCounterVec(prometheus.CounterOpts{Name: "loop_runtime_egress_bytes_total"}, nil)
`,
	})
	out := mustCheck(t, Config{RepoRoot: root, SiblingRoot: absentSibling(t)})

	if out.Pass {
		t.Fatal("a series declared ABSENT that the repo now emits must FAIL so the ledger is updated")
	}
	v := joined(out.Violations)
	if !strings.Contains(v, "NOW emits it") || !strings.Contains(v, "api/internal/metrics/egress.go") {
		t.Errorf("the violation must name the new producer, got:\n%s", v)
	}
}

// A metric named only in a test file is not a producer. Counting one would
// report the gap as closed while nothing emits the series in production.
func TestMetricNamedOnlyInATestFileIsNotAProducer(t *testing.T) {
	root := fakeRepo(t, threeLineDecl, map[string]string{
		"api/internal/metrics/metrics.go": metricsGo,
		"api/internal/metrics/egress_test.go": `package metrics

var fake = prometheus.CounterOpts{Name: "loop_runtime_egress_bytes_total"}
`,
	})
	out := mustCheck(t, Config{RepoRoot: root, SiblingRoot: absentSibling(t)})

	if !out.Pass {
		t.Fatalf("a _test.go mention must not close the gap; violations:\n%s", joined(out.Violations))
	}
	if len(out.TrackedAbsences) != 2 {
		t.Errorf("both absences must still be tracked, got %v", out.TrackedAbsences)
	}
}

// --- 3. the sibling is absent ----------------------------------------------

// The B-169 shape, asserted directly: with the sibling gone the gate must not
// claim it compared anything, and must say so in words.
func TestSiblingAbsent_ReportsUncheckedAndNamesWhat(t *testing.T) {
	root := fakeRepo(t, threeLineDecl, map[string]string{
		"api/internal/metrics/metrics.go": metricsGo,
	})
	out := mustCheck(t, Config{RepoRoot: root, SiblingRoot: absentSibling(t)})

	if out.SiblingChecked {
		t.Fatal("SiblingChecked must be false when loop-console is not on disk")
	}
	if out.SiblingSkipReason == "" {
		t.Fatal("an unchecked drift half must carry a reason -- a silent skip is the B-169 defect")
	}
	if !strings.Contains(out.SiblingSkipReason, "NOT compared") {
		t.Errorf("the reason must say what was not done, got %q", out.SiblingSkipReason)
	}
	if len(out.QueriedSeries) != 0 {
		t.Errorf("nothing was observed, so QueriedSeries must be empty, got %v", out.QueriedSeries)
	}
	if !out.Pass {
		t.Errorf("the hermetic producer half still holds, so the gate must not be red; violations:\n%s",
			joined(out.Violations))
	}
}

// --- drift, with the sibling present ---------------------------------------

func TestSiblingPresentAndMatching(t *testing.T) {
	root := fakeRepo(t, threeLineDecl, map[string]string{
		"api/internal/metrics/metrics.go": metricsGo,
	})
	out := mustCheck(t, Config{RepoRoot: root, SiblingRoot: fakeSibling(t, aggregatorWithThree)})

	if !out.Pass {
		t.Fatalf("a declaration that matches promQueries must pass; violations:\n%s", joined(out.Violations))
	}
	if !out.SiblingChecked {
		t.Fatal("SiblingChecked must be true when the comparison actually ran")
	}
	if len(out.QueriedSeries) != 3 {
		t.Errorf("expected the three queried series observed, got %v", out.QueriedSeries)
	}
	if out.SiblingSkipReason != "" {
		t.Errorf("a checked drift half must carry no skip reason, got %q", out.SiblingSkipReason)
	}
}

// The DoD bullet: a query naming a series nothing declares must fail. This is
// the NEXT mismatch, the one the pin exists to catch.
func TestAggregatorQueriesAnUndeclaredSeriesFails(t *testing.T) {
	agg := strings.Replace(aggregatorWithThree,
		"loop_runtime_docr_pulls_total", "loop_runtime_brand_new_cost_total", 1)
	root := fakeRepo(t, threeLineDecl, map[string]string{
		"api/internal/metrics/metrics.go": metricsGo,
	})
	out := mustCheck(t, Config{RepoRoot: root, SiblingRoot: fakeSibling(t, agg)})

	if out.Pass {
		t.Fatal("a series the aggregator queries and the pin does not declare must FAIL")
	}
	v := joined(out.Violations)
	if !strings.Contains(v, "loop_runtime_brand_new_cost_total") || !strings.Contains(v, "does not declare") {
		t.Errorf("the violation must name the undeclared series, got:\n%s", v)
	}
}

func TestStaleDeclarationFails(t *testing.T) {
	agg := strings.Replace(aggregatorWithThree,
		"sum by (tenant) (loop_runtime_docr_pulls_total)", "vector(0)", 1)
	root := fakeRepo(t, threeLineDecl, map[string]string{
		"api/internal/metrics/metrics.go": metricsGo,
	})
	out := mustCheck(t, Config{RepoRoot: root, SiblingRoot: fakeSibling(t, agg)})

	if out.Pass {
		t.Fatal("a pinned series the aggregator no longer queries must FAIL as stale")
	}
	if !strings.Contains(joined(out.Violations), "no longer queries") {
		t.Errorf("the violation must say the line is stale, got:\n%s", joined(out.Violations))
	}
}

// The sibling is HERE and the query source is not where it was. That is an
// observation, not an absence, so it fails rather than skipping.
func TestSiblingPresentButAggregatorMovedFails(t *testing.T) {
	sibling := t.TempDir() // a loop-console with no internal/billing at all
	root := fakeRepo(t, threeLineDecl, map[string]string{
		"api/internal/metrics/metrics.go": metricsGo,
	})
	out := mustCheck(t, Config{RepoRoot: root, SiblingRoot: sibling})

	if out.Pass {
		t.Fatal("loop-console present with the aggregator missing must FAIL, not skip")
	}
	if out.SiblingChecked {
		t.Error("nothing was compared, so SiblingChecked must be false")
	}
	if !strings.Contains(joined(out.Violations), "cannot be read") {
		t.Errorf("the violation must name the unreadable aggregator, got:\n%s", joined(out.Violations))
	}
}

func TestPromQueriesMapRenamedFails(t *testing.T) {
	agg := strings.Replace(aggregatorWithThree, "var promQueries", "var costQueries", 1)
	root := fakeRepo(t, threeLineDecl, map[string]string{
		"api/internal/metrics/metrics.go": metricsGo,
	})
	out := mustCheck(t, Config{RepoRoot: root, SiblingRoot: fakeSibling(t, agg)})

	if out.Pass {
		t.Fatal("losing sight of the query surface must FAIL, not pass quietly")
	}
	if !strings.Contains(joined(out.Violations), "no `var promQueries` map") {
		t.Errorf("the violation must say the map was not found, got:\n%s", joined(out.Violations))
	}
}

// The anti-vacuity floor: an extractor that matches nothing would make every
// drift comparison a no-op and every run green.
func TestPromQueriesWithNoLoopRuntimeSeriesFails(t *testing.T) {
	agg := `package billing

var promQueries = map[planpkg.CostMetric]string{
	planpkg.MetricEgressBytes: ` + "`sum by (tenant) (container_network_transmit_bytes_total)`" + `,
}
`
	root := fakeRepo(t, threeLineDecl, map[string]string{
		"api/internal/metrics/metrics.go": metricsGo,
	})
	out := mustCheck(t, Config{RepoRoot: root, SiblingRoot: fakeSibling(t, agg)})

	if out.Pass {
		t.Fatal("a promQueries map yielding zero loop_runtime_* series must FAIL as a broken extractor")
	}
	if !strings.Contains(joined(out.Violations), "names no loop_runtime_* series") {
		t.Errorf("the violation must name the empty extraction, got:\n%s", joined(out.Violations))
	}
	if out.SiblingChecked {
		t.Error("nothing usable was observed, so SiblingChecked must be false")
	}
}

// promQueriesBlock must stop at the brace that balances the opening one, not at
// the first `}` it meets -- the entries contain none today, but a query with a
// label matcher would.
func TestPromQueriesBlockBalancesBraces(t *testing.T) {
	src := `var promQueries = map[k]string{
	A: ` + "`sum(loop_runtime_a_total{env=\"prod\"})`" + `,
	B: ` + "`sum(loop_runtime_b_total)`" + `,
}
var after = "loop_runtime_not_a_query_total"
`
	block, ok := promQueriesBlock(src)
	if !ok {
		t.Fatal("the map must be found")
	}
	got := seriesIn(block)
	want := []string{"loop_runtime_a_total", "loop_runtime_b_total"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("brace counting must include the label-matcher entry and stop before `after`; got %v", got)
	}
}

// --- setup errors ----------------------------------------------------------

func TestDeclarationFileMissingIsAnError(t *testing.T) {
	if _, err := Check(Config{RepoRoot: t.TempDir()}); err == nil {
		t.Fatal("a missing pin must be an ERROR -- a gate with no contract asserts nothing")
	}
}

func TestEmptyDeclarationIsAnError(t *testing.T) {
	root := fakeRepo(t, "# only comments\n\n", nil)
	_, err := Check(Config{RepoRoot: root, SiblingRoot: absentSibling(t)})
	if !errors.Is(err, errNoDeclarations) {
		t.Fatalf("an empty pin must be rejected, got err=%v", err)
	}
}

func TestMalformedDeclarationLineIsAnError(t *testing.T) {
	root := fakeRepo(t, "loop_runtime_x_total MAYBE somebody\n", nil)
	_, err := Check(Config{RepoRoot: root, SiblingRoot: absentSibling(t)})
	if err == nil || !strings.Contains(err.Error(), "cannot parse") {
		t.Fatalf("an unparseable line must be rejected, got err=%v", err)
	}
}

func TestDuplicateSeriesIsAnError(t *testing.T) {
	root := fakeRepo(t, "loop_runtime_x_total ABSENT B-1\nloop_runtime_x_total PRODUCER a.go\n", nil)
	_, err := Check(Config{RepoRoot: root, SiblingRoot: absentSibling(t)})
	if err == nil || !strings.Contains(err.Error(), "declared twice") {
		t.Fatalf("two answers for one series must be rejected, got err=%v", err)
	}
}

// --- the real tree ---------------------------------------------------------

const repoRootFromHere = "../../../../.."

// TestRealTree_ProducerContractHolds anchors the synthetic fixtures above to
// the repository they are about. The sibling is pointed at a path that cannot
// exist, so this asserts ONLY the hermetic half -- which is exactly the half
// that must hold on a machine without loop-console.
func TestRealTree_ProducerContractHolds(t *testing.T) {
	out, err := Check(Config{RepoRoot: repoRootFromHere, SiblingRoot: absentSibling(t)})
	if err != nil {
		t.Fatalf("Check against the real tree: %v", err)
	}
	if !out.Pass {
		t.Fatalf("the producer contract must hold in this tree; violations:\n%s", joined(out.Violations))
	}
	if len(out.Declarations) != 3 {
		t.Fatalf("the aggregator runs three queries (cost_aggregator.go:82-86); the pin declares %d",
			len(out.Declarations))
	}
	if len(out.TrackedAbsences) != 2 {
		t.Errorf("B-181 measured two series nobody emits; the pin tracks %d: %v",
			len(out.TrackedAbsences), out.TrackedAbsences)
	}
	if len(out.VerifiedProducers) != 1 {
		t.Errorf("one of the three has a producer here (api/internal/metrics/build_metrics.go); verified %d: %v",
			len(out.VerifiedProducers), out.VerifiedProducers)
	}
}

// The scan that decides "nobody emits this" must actually see this repo's
// metrics. A walker that silently found nothing would make every absence look
// tracked and every gap look open forever.
func TestRealTree_EmitterScanSeesThisRepo(t *testing.T) {
	emitters, err := scanEmitters(repoRootFromHere)
	if err != nil {
		t.Fatalf("scanEmitters: %v", err)
	}
	if len(emitters) < 50 {
		t.Fatalf("the emitter scan found %d loop_runtime_* metric declarations; the tree had 128 unique ones "+
			"measured 2026-09-05, so a number this low means the walk is not reaching the sources", len(emitters))
	}
	if got := emitters["loop_runtime_build_duration_seconds"]; got != "api/internal/metrics/build_metrics.go" {
		t.Errorf("loop_runtime_build_duration_seconds must be found in api/internal/metrics/build_metrics.go, got %q", got)
	}
	if got, ok := emitters["loop_runtime_egress_bytes_total"]; ok {
		t.Errorf("loop_runtime_egress_bytes_total must have NO producer in this tree (B-181), found %q", got)
	}
	if got, ok := emitters["loop_runtime_docr_pulls_total"]; ok {
		t.Errorf("loop_runtime_docr_pulls_total must have NO producer in this tree (B-181), found %q", got)
	}
}
