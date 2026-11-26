// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package helmtopology

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture builds a throwaway repo root with the given helmfiles and infra/helm dirs.
func fixture(t *testing.T, helmfiles map[string]string, helmDirs map[string]bool) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, helmfileDir), 0o750); err != nil {
		t.Fatal(err)
	}
	for name, body := range helmfiles {
		p := filepath.Join(root, helmfileDir, name+".helmfile.yaml.gotmpl")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for dir, withChartYaml := range helmDirs {
		d := filepath.Join(root, helmDir, dir)
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
		if withChartYaml {
			if err := os.WriteFile(filepath.Join(d, "Chart.yaml"), []byte("name: x\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

func entryFor(t *testing.T, rep *Report, chart string) Entry {
	t.Helper()
	for _, e := range rep.Entries {
		if e.Chart == chart {
			return e
		}
	}
	t.Fatalf("chart %q not in index; got %d entries", chart, len(rep.Entries))
	return Entry{}
}

// --- EC-1: the source has TWO declaration forms; reading one covers 19 of 44 ---------------

func TestExtractsUpstreamByValuesPath(t *testing.T) {
	root := fixture(t, map[string]string{
		"runtime": `
releases:
  - name: keda
    namespace: keda
    chart: kedacore/keda
    version: 2.16.1
    values:
      - ../helm/keda/values.yaml
`,
	}, map[string]bool{"keda": false})

	rep, err := Check(root, ModeCheck, "doc.md")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	e := entryFor(t, rep, "keda")
	if e.Nature != NatureUpstream {
		t.Errorf("keda nature = %q, want upstream (EC-1: declared via remote chart + local values)", e.Nature)
	}
	if len(e.Clusters) != 1 || e.Clusters[0] != "runtime" {
		t.Errorf("keda clusters = %v, want [runtime]", e.Clusters)
	}
	if e.Version != "2.16.1" {
		t.Errorf("keda version = %q, want 2.16.1 — the pinned upstream version is free information", e.Version)
	}
}

func TestClassifiesByDeclarationForm(t *testing.T) {
	root := fixture(t, map[string]string{
		"mgmt": `
releases:
  - name: loop-runtime-api
    chart: ../helm/loop-runtime-api
  - name: argocd
    chart: argo/argo-cd
    version: 7.3.11
    values:
      - ../helm/argocd/values.yaml
`,
	}, map[string]bool{"loop-runtime-api": true, "argocd": false})

	rep, _ := Check(root, ModeCheck, "doc.md")
	if got := entryFor(t, rep, "loop-runtime-api").Nature; got != NatureOwn {
		t.Errorf("loop-runtime-api nature = %q, want ours", got)
	}
	if got := entryFor(t, rep, "argocd").Nature; got != NatureUpstream {
		t.Errorf("argocd nature = %q, want upstream", got)
	}
}

// --- EC-2: conditionals are reproduced, never evaluated -------------------------------------

func TestPreservesConditionalLiterally(t *testing.T) {
	root := fixture(t, map[string]string{
		"mgmt": `
releases:
  - name: postgresql
    chart: bitnami/postgresql
    version: 15.5.0
    installed: {{ .Values.inClusterPostgres | default false }}
    values:
      - ../helm/postgresql/values.yaml
`,
	}, map[string]bool{"postgresql": false})

	rep, _ := Check(root, ModeCheck, "doc.md")
	e := entryFor(t, rep, "postgresql")
	if !strings.Contains(e.Conditional, "inClusterPostgres") {
		t.Errorf("conditional = %q, want the literal predicate — evaluating it would mean "+
			"picking one environment and lying about the others (EC-2)", e.Conditional)
	}
	if e.Conditional == "true" || e.Conditional == "false" {
		t.Error("conditional was evaluated to a boolean; it must stay literal")
	}
}

// --- EC-3: a missing document is actionable, not a raw I/O error ----------------------------

func TestMissingDocumentGivesInstruction(t *testing.T) {
	root := fixture(t, map[string]string{
		"mgmt": "releases:\n  - name: a\n    chart: ../helm/a\n",
	}, map[string]bool{"a": true})

	rep, err := Check(root, ModeCheck, "does-not-exist.md")
	if err != nil {
		t.Fatalf("missing doc must NOT be a raw error: %v", err)
	}
	if rep.InSync {
		t.Error("missing doc must not report in-sync")
	}
	if !strings.Contains(rep.Message, "--write") {
		t.Errorf("message = %q, want it to cite --write; the gate runs on git push and an "+
			"unhelpful error blocks every push (EC-3)", rep.Message)
	}
}

// --- EC-4 / EC-5: the directory is the key, not the release name ----------------------------

func TestMultipleValuesSameDirectoryDoesNotDuplicate(t *testing.T) {
	root := fixture(t, map[string]string{
		"mgmt": `
releases:
  - name: admission-policies-mgmt
    chart: ../helm/admission-policies
    values:
      - ../helm/admission-policies/values.yaml
      - ../helm/admission-policies/values-mgmt.yaml
`,
	}, map[string]bool{"admission-policies": true})

	rep, _ := Check(root, ModeCheck, "doc.md")
	n := 0
	for _, e := range rep.Entries {
		if e.Chart == "admission-policies" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("admission-policies appears %d times, want 1 (EC-4)", n)
	}
}

func TestTwoReleasesSameDirectoryAggregateClusters(t *testing.T) {
	root := fixture(t, map[string]string{
		"mgmt":    "releases:\n  - name: admission-policies-mgmt\n    chart: ../helm/admission-policies\n",
		"runtime": "releases:\n  - name: admission-policies\n    chart: ../helm/admission-policies\n",
	}, map[string]bool{"admission-policies": true})

	rep, _ := Check(root, ModeCheck, "doc.md")
	e := entryFor(t, rep, "admission-policies")
	if len(e.Clusters) != 2 || e.Clusters[0] != "mgmt" || e.Clusters[1] != "runtime" {
		t.Errorf("clusters = %v, want [mgmt runtime] — the key is the DIRECTORY, not the "+
			"release name (EC-5)", e.Clusters)
	}
}

// --- EC-6: an undeclared directory is listed, never silently omitted ------------------------

func TestOrphanDirectoryAppearsMarked(t *testing.T) {
	root := fixture(t, map[string]string{
		"mgmt": "releases:\n  - name: a\n    chart: ../helm/a\n",
	}, map[string]bool{"a": true, "orphan": true})

	rep, _ := Check(root, ModeCheck, "doc.md")
	e := entryFor(t, rep, "orphan")
	if e.Nature != NatureUndeclared {
		t.Errorf("orphan nature = %q, want %q — omitting would hide dead charts (EC-6)",
			e.Nature, NatureUndeclared)
	}
	if len(e.Clusters) != 0 {
		t.Errorf("orphan clusters = %v, want empty", e.Clusters)
	}
}

// A chart deleted from git leaves its downloaded dependencies behind: `charts/`
// under infra/helm is gitignored, so `git rm` keeps the directory alive on every
// machine that ever ran `helm dependency build`. Listing it as undeclared made
// the index depend on local artifacts — measured 2026-09-25 after infra/helm/harbor
// was deleted, with only charts/harbor-1.16.0.tgz left on disk.
func TestDirectoryHoldingOnlyDownloadedDependenciesIsNotAChart(t *testing.T) {
	root := fixture(t, map[string]string{
		"mgmt": "releases:\n  - name: a\n    chart: ../helm/a\n",
	}, map[string]bool{"a": true, "leftover": false})
	deps := filepath.Join(root, helmDir, "leftover", "charts")
	if err := os.MkdirAll(deps, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deps, "leftover-1.0.0.tgz"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	rep, err := Check(root, ModeCheck, "doc.md")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	for _, e := range rep.Entries {
		if e.Chart == "leftover" {
			t.Fatalf("a directory holding only downloaded dependencies was indexed as %q", e.Nature)
		}
	}
}

// --- the helmfiles are .gotmpl: any YAML parser fails on them -------------------------------

func TestDoesNotParseGotmplAsYAML(t *testing.T) {
	root := fixture(t, map[string]string{
		"mgmt": `
{{- if eq .Environment.Name "prod" }}
releases:
  - name: a
    chart: ../helm/a
{{- end }}
`,
	}, map[string]bool{"a": true})

	rep, err := Check(root, ModeCheck, "doc.md")
	if err != nil {
		t.Fatalf("Go template syntax must not break extraction: %v", err)
	}
	entryFor(t, rep, "a") // fails the test if absent
}

// --- D2: the date must not participate in the verdict ---------------------------------------

func TestComparisonIgnoresDateStamp(t *testing.T) {
	root := fixture(t, map[string]string{
		"mgmt": "releases:\n  - name: a\n    chart: ../helm/a\n",
	}, map[string]bool{"a": true})

	// Write once, then hand-edit a date OUTSIDE the markers. Same body ⇒ still in sync.
	if _, err := Check(root, ModeWrite, "doc.md"); err != nil {
		t.Fatalf("write: %v", err)
	}
	p := filepath.Join(root, "doc.md")
	raw, _ := os.ReadFile(p) //nolint:gosec // test fixture
	edited := strings.Replace(string(raw), "# Helm Topology",
		"# Helm Topology\n\nGenerated on 1999-01-01.", 1)
	if err := os.WriteFile(p, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}

	rep, err := Check(root, ModeCheck, "doc.md")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !rep.InSync {
		t.Errorf("a date outside the table must not affect the verdict — this is the defect "+
			"that blocks a push every midnight in the sibling gate (D2). msg=%q", rep.Message)
	}
}

func TestFailsWhenNewChartIsNotInIndex(t *testing.T) {
	root := fixture(t, map[string]string{
		"mgmt": "releases:\n  - name: a\n    chart: ../helm/a\n",
	}, map[string]bool{"a": true})
	if _, err := Check(root, ModeWrite, "doc.md"); err != nil {
		t.Fatal(err)
	}

	// A new chart lands in a helmfile; the committed index no longer covers the source.
	hf := filepath.Join(root, helmfileDir, "core.helmfile.yaml.gotmpl")
	if err := os.WriteFile(hf,
		[]byte("releases:\n  - name: a\n    chart: ../helm/a\n  - name: b\n    chart: ../helm/b\n"),
		0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, helmDir, "b"), 0o750); err != nil {
		t.Fatal(err)
	}

	rep, _ := Check(root, ModeCheck, "doc.md")
	if rep.InSync {
		t.Error("index must go stale when a chart is added — a gate that never fails proves nothing")
	}
	if !strings.Contains(rep.Message, "--write") {
		t.Errorf("message = %q, want it to say how to fix", rep.Message)
	}
}

// --- pipes inside a cell break the markdown table ------------------------------------------
//
// Found running against the real repo: `installed: {{ .Values.inClusterPostgres | default false }}`
// carries a `|`, which markdown reads as a column separator. Five rows rendered with six columns.
// Fixture tests never caught it because no fixture had a pipe in the predicate.
func TestEscapesPipeInsideCell(t *testing.T) {
	root := fixture(t, map[string]string{
		"mgmt": `
releases:
  - name: postgresql
    chart: bitnami/postgresql
    version: 18.6.2
    installed: {{ .Values.inClusterPostgres | default false }}
    values:
      - ../helm/postgresql/values.yaml
`,
	}, map[string]bool{"postgresql": false})

	rep, err := Check(root, ModeCheck, "doc.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(rep.Body, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		// A 5-column row has exactly 6 unescaped pipes: leading, 4 separators, trailing.
		bare := strings.Count(line, "|") - strings.Count(line, `\|`)
		if bare != 6 {
			t.Errorf("row has %d unescaped pipes, want 6 — an unescaped pipe in a cell splits "+
				"the markdown table into a phantom column:\n  %s", bare, line)
		}
	}
}

// A chart Helmfile installs straight from charts/ is ours and runs on that cluster.
// When a bootstrap/published pair became one chart, the release moved from
// `chart: ../helm/<name>` to `chart: ../../charts/<name>`; reading only the first
// form dropped the release from the index while it kept running.
func TestIndexesAChartInstalledFromTheChartsTree(t *testing.T) {
	root := fixture(t, map[string]string{
		"core": `
releases:
  - name: loop-runtime-api
    installed: {{ .Values.apiEnabled }}
    chart: ../../charts/loop-runtime-api
    values:
      - ../../charts/loop-runtime-api/values-bootstrap.yaml
`,
	}, map[string]bool{"cloudflared": true})

	rep, err := Check(root, ModeCheck, "doc.md")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	e := entryFor(t, rep, "loop-runtime-api")
	if e.Nature != NatureOwn {
		t.Errorf("loop-runtime-api nature = %q, want ours", e.Nature)
	}
	if len(e.Clusters) != 1 || e.Clusters[0] != "core" {
		t.Errorf("loop-runtime-api clusters = %v, want [core]", e.Clusters)
	}
	if e.Conditional != "{{ .Values.apiEnabled }}" {
		t.Errorf("loop-runtime-api conditional = %q, want the literal predicate", e.Conditional)
	}
}
