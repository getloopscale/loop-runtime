// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package helmdrift

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// B-001 RED tests.
//
// CLAUDE.md makes the two `loopzero-api` charts a parity contract — "when patching one,
// apply the same fix to the other". Every gate that runs over that pair compares
// EXISTENCE: `chart-pairs` compares the set of declared names (and its own package
// doc says content parity "is another problem"), and `helm-drift` compares the set
// of resource KINDS each side installs. Neither reads what either side GRANTS.
//
// Two tests, deliberately at different levels:
//
//	TestDivergentGrantsInDeclaredPairAreRefused — hermetic. Proves the gate cannot
//	    detect a grant divergence AT ALL. Owns its own fixture, so it can never go
//	    green because someone changed a chart.
//
//	TestRealLoopZeroAPIPairRBACDivergenceIsReported — reads the REAL loopzero-api pair and
//	    checks the gate against an INDEPENDENTLY written oracle. Proves the gate
//	    agrees with a second implementation, not with itself.
//
// Both are RED until the cell comparison lands, and they were committed RED on
// purpose (B-001 FR-006): an acceptance criterion that greps a commit for a test
// name is unfalsifiable while the file is untracked.

// ---------------------------------------------------------------------------
// Hermetic: the gate cannot see a grant divergence at all.
// ---------------------------------------------------------------------------

// bootstrapClusterRole and publishedClusterRole reproduce the real pair's
// `secrets` divergence exactly: both sides grant get/list/watch/create/update/
// patch, and ONLY the bootstrap side adds `delete`. One verb, which is the
// smallest signal the gate has to catch — measured on the real charts at
// infra/helm/loopzero-api/templates/serviceaccount.yaml (7 verbs incl. delete) and
// charts/loopzero-api/templates/clusterrole.yaml (the same 6, without delete).
//
// The fixture pair is named `loopzero-rbacprobe`, NOT `loopzero-api`: a fixture sharing
// the real pair's name would be answered by the real pair's registry entries, and
// the test would then measure the registry rather than the comparison.
const bootstrapClusterRole = `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: probe
rules:
  - apiGroups: [""]
    resources: [secrets]
    verbs: [get, list, watch, create, update, patch, delete]
`

const publishedClusterRole = `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: probe
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "watch", "create", "update", "patch"]
`

// writeChart drops one rendered document into a fixture chart's templates/ dir.
// fixtureRenderer (check_test.go) reads those files back as though they were the
// rendered stream, so the fixtures need no Chart.yaml and no helm.
func writeChart(t *testing.T, root, chartPath, file, body string) {
	t.Helper()
	dir := filepath.Join(root, chartPath, "templates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// FAILS on the current state: the gate compares resource KINDS, both sides
// install exactly `ClusterRole`, so it reports PASS over a one-verb divergence.
func TestDivergentGrantsInDeclaredPairAreRefused(t *testing.T) {
	root := t.TempDir()
	writeChart(t, root, "infra/helm/loopzero-rbacprobe", "rbac.yaml", bootstrapClusterRole)
	writeChart(t, root, "charts/loopzero-rbacprobe", "rbac.yaml", publishedClusterRole)

	out, err := Check(Config{RepoRoot: root, Renderer: fixtureRenderer{}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatalf("the pair diverges on exactly one cell — (core)/secrets/delete, granted by " +
			"infra/helm and withheld by charts/ — and the gate reported PASS. Both sides install " +
			"exactly one ClusterRole, so a comparison of resource KINDS can never see it.")
	}

	// NFR-002: the finding names the fully-qualified triple. A count, or a bare
	// boolean, tells a reader that something diverged and not whether it is
	// progress or regression.
	var lines []string
	for _, d := range out.RBACDrifts {
		lines = append(lines, d.ReportLines()...)
	}
	joined := strings.Join(lines, "\n")
	t.Logf("RBAC report:\n%s", joined)
	if !strings.Contains(joined, "(core)/secrets/delete") {
		t.Errorf("the report does not name (core)/secrets/delete; it read:\n%s", joined)
	}
}

// ---------------------------------------------------------------------------
// Real pair: the gate must agree with an independently written oracle.
// ---------------------------------------------------------------------------

// oracleRule / oracleDoc are typed, where the gate decodes into map[string]any.
// The difference is deliberate: the oracle must be a SECOND implementation, or
// the agreement below is a function agreeing with itself.
type oracleRule struct {
	APIGroups []string `yaml:"apiGroups"`
	Resources []string `yaml:"resources"`
	Verbs     []string `yaml:"verbs"`
}

type oracleDoc struct {
	Kind  string       `yaml:"kind"`
	Rules []oracleRule `yaml:"rules"`
}

// oracleCells renders one chart and expands every ClusterRole rule into its
// (apiGroup, resource, verb) cells. It shells `helm` ITSELF rather than using the
// injected Renderer: a bug in the renderer would otherwise move both sides
// identically and the agreement check would pass while both were wrong.
func oracleCells(t *testing.T, chartDir string, sets []string) map[string]bool {
	t.Helper()

	// NOT t.Skip. This test exists to measure the real charts, and a skip would
	// report green having measured nothing — the precise failure class B-001 is
	// about. helm is a declared prerequisite of this repository (CLAUDE.md,
	// Helm v3.14+).
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatalf("helm is not on PATH and this test measures real rendered charts: %v", err)
	}

	args := []string{"template", "b001-oracle", chartDir}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	out, err := exec.Command("helm", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm %s: %v\n%s\n(if this names a missing dependency, run "+
			"`helm dependency build` on the chart first)", strings.Join(args, " "), err, out)
	}

	cells := map[string]bool{}
	dec := yaml.NewDecoder(strings.NewReader(string(out)))
	for {
		var doc oracleDoc
		derr := dec.Decode(&doc)
		if errors.Is(derr, io.EOF) {
			break
		}
		if derr != nil {
			t.Fatalf("parsing rendered manifest of %s: %v", chartDir, derr)
		}
		if doc.Kind != "ClusterRole" {
			continue
		}
		for _, r := range doc.Rules {
			groups := r.APIGroups
			if len(groups) == 0 {
				groups = []string{""}
			}
			for _, g := range groups {
				label := g
				if label == "" {
					label = "(core)"
				}
				for _, res := range r.Resources {
					for _, v := range r.Verbs {
						cells[label+"/"+res+"/"+v] = true
					}
				}
			}
		}
	}

	if len(cells) == 0 {
		t.Fatalf("no ClusterRole cells rendered from %s — measuring nothing is not the "+
			"same as measuring zero divergence", chartDir)
	}
	return cells
}

// The gate's finding on the REAL loopzero-api pair must agree with the oracle, cell
// for cell, in both directions.
//
// Written as AGREEMENT rather than as "must always fail" on purpose. B-001 allows
// the pair to be resolved by CONVERGING it, and a test asserting unconditional
// failure would go green on that day without the gate having been fixed — a test
// that silently stops testing. Agreement stays meaningful either way: diverging
// pair => the gate must report the same cells; converged pair => both are silent.
//
// FAILS on the current state: the oracle finds 174 divergent cells over the
// pinned matrix and the gate reports none.
func TestRealLoopZeroAPIPairRBACDivergenceIsReported(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "..")

	// The oracle unions the DIVERGENCES over the matrix, not the cell sets: a
	// cell the published chart grants only under one flag state still diverges
	// under the other.
	boot := oracleCells(t, filepath.Join(root, "infra/helm/loopzero-api"), nil)
	divergent := map[string]bool{}
	for _, sets := range matrixFor("loopzero-api:loopzero-api") {
		pub := oracleCells(t, filepath.Join(root, "charts/loopzero-api"), sets)
		for c := range boot {
			if !pub[c] {
				divergent["bootstrap-only "+c] = true
			}
		}
		for c := range pub {
			if !boot[c] {
				divergent["published-only "+c] = true
			}
		}
	}

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.RenderErrors) > 0 {
		t.Fatalf("the gate could not render the pair, so nothing was measured: %s",
			strings.Join(out.RenderErrors, "; "))
	}

	gate := map[string]bool{}
	for _, d := range out.RBACDrifts {
		if d.Pair != "loopzero-api:loopzero-api" {
			continue
		}
		for _, c := range d.OnlyInBootstrap {
			gate["bootstrap-only "+c.String()] = true
		}
		for _, c := range d.OnlyInPublished {
			gate["published-only "+c.String()] = true
		}
	}

	var disagree []string
	for c := range divergent {
		if !gate[c] {
			disagree = append(disagree, "oracle only: "+c)
		}
	}
	for c := range gate {
		if !divergent[c] {
			disagree = append(disagree, "gate only: "+c)
		}
	}
	sort.Strings(disagree)

	if len(disagree) > 0 {
		sample := disagree
		if len(sample) > 10 {
			sample = sample[:10]
		}
		t.Fatalf("oracle: the declared loopzero-api pair diverges on %d (apiGroup, resource, verb) "+
			"cells over the pinned matrix; the gate reported %d. %d disagreements, first %d:\n  %s",
			len(divergent), len(gate), len(disagree), len(sample), strings.Join(sample, "\n  "))
	}
	// Agreement is exact, and the passing run says so with its own numbers
	// (NFR-004) — a test that prints nothing cannot be told from one that ran
	// nothing.
	t.Logf("divergent cells: %d; disagree: %d", len(divergent), len(disagree))
}
