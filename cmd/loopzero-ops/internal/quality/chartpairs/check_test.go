// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package chartpairs_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/chartpairs"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

// chart describes one fixture chart: its Chart.yaml name and its templates.
type chart struct {
	name      string
	templates map[string]string
}

func tree(t *testing.T, infraHelm, charts map[string]chart) string {
	t.Helper()
	root := t.TempDir()
	write := func(base, dir string, c chart) {
		chartDir := filepath.Join(root, base, dir)
		if err := os.MkdirAll(filepath.Join(chartDir, "templates"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"),
			[]byte("apiVersion: v2\nname: "+c.name+"\nversion: 0.1.0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		for rel, body := range c.templates {
			p := filepath.Join(chartDir, "templates", rel)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for dir, c := range infraHelm {
		write("infra/helm", dir, c)
	}
	for dir, c := range charts {
		write("charts", dir, c)
	}
	return root
}

var policyTemplates = map[string]string{
	"vap.yaml":     "kind: ValidatingAdmissionPolicy\n",
	"binding.yaml": "kind: ValidatingAdmissionPolicyBinding\n",
}

// Distinct charts in the two trees are the normal state and pass.
func TestDistinctChartsPass(t *testing.T) {
	root := tree(t,
		map[string]chart{"gitea": {name: "gitea", templates: map[string]string{"a.yaml": "kind: A\n"}}},
		map[string]chart{"loopzero-api": {name: "loopzero-api", templates: map[string]string{"d.yaml": "kind: Deployment\n"}}})

	out, err := violationOf(chartpairs.Check(chartpairs.Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("no chart exists twice; got %v", err)
	}
	if out.InfraHelm != 1 || out.Charts != 1 {
		t.Errorf("want 1 chart in each tree, got %d and %d", out.InfraHelm, out.Charts)
	}
}

// The same chart name in both trees is the pair shape the decision retired.
func TestSameNameInBothTreesIsRefused(t *testing.T) {
	root := tree(t,
		map[string]chart{"loopzero-api": {name: "loopzero-api", templates: map[string]string{"x.yaml": "kind: X\n"}}},
		map[string]chart{"loopzero-api": {name: "loopzero-api", templates: map[string]string{"y.yaml": "kind: Y\n"}}})

	out, err := violationOf(chartpairs.Check(chartpairs.Config{RepoRoot: root}))
	if !errors.Is(err, chartpairs.ErrDuplicated) {
		t.Fatalf("want ErrDuplicated, got %v", err)
	}
	if len(out.Duplicates) != 1 || out.Duplicates[0] != "infra/helm/loopzero-api ↔ charts/loopzero-api (same name)" {
		t.Errorf("want the pair named with both paths, got %v", out.Duplicates)
	}
}

// A copy under another name — the admission-policies shape — cannot be found by
// name, so it is found by content: the same template set, byte for byte.
func TestTemplateIdenticalCopyUnderAnotherNameIsRefused(t *testing.T) {
	root := tree(t,
		map[string]chart{"admission-policies": {name: "admission-policies", templates: policyTemplates}},
		map[string]chart{"loopzero-admission-policies": {name: "loopzero-admission-policies", templates: policyTemplates}})

	out, err := violationOf(chartpairs.Check(chartpairs.Config{RepoRoot: root}))
	if !errors.Is(err, chartpairs.ErrDuplicated) {
		t.Fatalf("want ErrDuplicated, got %v", err)
	}
	want := "infra/helm/admission-policies ↔ charts/loopzero-admission-policies (identical templates)"
	if len(out.Duplicates) != 1 || out.Duplicates[0] != want {
		t.Errorf("want %q, got %v", want, out.Duplicates)
	}
}

// One differing template is a different chart, not a copy: the content check
// matches whole template sets only, so it cannot flag two charts that share a file.
func TestChartsSharingOnlySomeTemplatesAreNotACopy(t *testing.T) {
	variant := map[string]string{"vap.yaml": policyTemplates["vap.yaml"], "binding.yaml": "kind: Other\n"}
	root := tree(t,
		map[string]chart{"admission-policies": {name: "admission-policies", templates: policyTemplates}},
		map[string]chart{"loopzero-admission-policies": {name: "loopzero-admission-policies", templates: variant}})

	if _, err := violationOf(chartpairs.Check(chartpairs.Config{RepoRoot: root})); err != nil {
		t.Fatalf("two charts with different template sets are two charts; got %v", err)
	}
}

// A chart with no templates (a library, a values-only wrapper) has no content to
// compare, and two of them must not be reported as copies of each other.
func TestChartsWithoutTemplatesAreNotComparedByContent(t *testing.T) {
	root := tree(t,
		map[string]chart{"wrapper": {name: "wrapper"}},
		map[string]chart{"loopzero-common": {name: "loopzero-common"}})

	if _, err := violationOf(chartpairs.Check(chartpairs.Config{RepoRoot: root})); err != nil {
		t.Fatalf("empty template sets are not evidence of a copy; got %v", err)
	}
}

// No charts at all is refused: zero with zero must not read as "scanned clean".
func TestNoChartsAtAllIsRefused(t *testing.T) {
	root := t.TempDir()
	if _, err := violationOf(chartpairs.Check(chartpairs.Config{RepoRoot: root})); !errors.Is(err, chartpairs.ErrNothingScanned) {
		t.Fatalf("want ErrNothingScanned, got %v", err)
	}
}

// The real population of this repository holds no chart twice.
func TestRealTree(t *testing.T) {
	out, err := violationOf(chartpairs.Check(chartpairs.Config{RepoRoot: qtestutil.Root(t)}))
	if err != nil {
		t.Fatalf("real tree holds a chart twice: %v (duplicates=%v)", err, out.Duplicates)
	}
	if out.InfraHelm == 0 || out.Charts == 0 {
		t.Fatalf("scanned %d chart(s) in infra/helm and %d in charts/ — green over nothing", out.InfraHelm, out.Charts)
	}
}
