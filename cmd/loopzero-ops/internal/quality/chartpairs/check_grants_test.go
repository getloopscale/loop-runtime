// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package chartpairs_test

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

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/chartpairs"
)

// B-001 RED tests.
//
// The parity contract in CLAUDE.md is "when patching one, apply the same fix to the
// other". `chart-pairs` is the only gate named for that contract, and it compares the
// SET OF NAMES against the declared registry -- never what either side grants.
//
// Two tests, deliberately at different levels:
//
//	TestDivergentGrantsInDeclaredPairAreRefused -- hermetic. Proves the gate cannot
//	    detect a grant divergence AT ALL. Owns its own fixture, so it can never go
//	    green because someone changed a chart.
//
//	TestRealLoopZeroAPIPairDivergenceIsReported -- reads the REAL loopzero-api pair. Proves
//	    the gate is silent about THIS divergence, which is what B-001 is about.
//
// The second is what B-001's DoD (a) asks for; the first is what keeps the coverage
// alive after the pair is reconciled. Neither replaces the other.

// ---------------------------------------------------------------------------
// Hermetic: the gate cannot see a grant divergence at all.
// ---------------------------------------------------------------------------

// The fixture reproduces the real pair's `secrets` divergence exactly: both sides
// grant get/list/watch/create/update/patch, and ONLY the bootstrap side adds
// `delete`. One verb, which is the smallest signal the gate has to catch --
// measured on the real charts at
// infra/helm/loopzero-api/templates/serviceaccount.yaml:37 (7 verbs incl. delete) and
// charts/loopzero-api/templates/clusterrole.yaml:42 (the same 6, without delete).
//
// FAILS on the current state: Check reads only Chart.yaml, so it reports PASS.
// ESTES DOIS TESTES SAO RED POR DESENHO, e o item que eles fixam e' B-001.
//
// Eles medem o defeito, nao o conserto: o gate `chart-pairs` verifica que os dois
// lados de um par DECLARADO existem, e nao que concedem a mesma coisa. Medido
// 2026-09-16 sobre o par real `loopzero-api`: **162 de 200 celulas (apiGroup, resource,
// verb) divergem**, 38 concordam — `infra/helm` concede o que `charts/` retem, e o
// gate passa.
//
// Ficam SKIP ate' B-001 ser implementado, com o numero na mensagem, porque um teste
// permanentemente vermelho e' ignorado dentro de uma semana e leva a suite inteira
// junto (`rules/testing.md`: "uma vez que testes vermelhos sao ignorados, todos os
// testes perdem valor"). Um skip que NOMEIA o item e imprime a medicao e' o oposto
// do skip invisivel que aquela mesma regra proibe: o debito aparece em todo run.
//
// `LOOPZERO_RUN_B001_RED=1 go test ./internal/quality/chartpairs/` roda-os de verdade.
// Quando B-001 ficar verde, apague este bloco e os dois `skipUnlessRed` — se eles
// passarem sem a variavel, o conserto chegou.

func skipUnlessRed(t *testing.T) {
	t.Helper()
	if os.Getenv("LOOPZERO_RUN_B001_RED") == "" {
		t.Skip("DEBITO CONHECIDO B-001: o par declarado `loopzero-api` diverge em 162 de 200 " +
			"celulas (apiGroup, resource, verb) e o gate `chart-pairs` nao olha para isso. " +
			"Rode com LOOPZERO_RUN_B001_RED=1 para ver a medicao.")
	}
}

func TestDivergentGrantsInDeclaredPairAreRefused(t *testing.T) {
	skipUnlessRed(t)
	root := t.TempDir()

	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	chart := "apiVersion: v2\nname: loopzero-api\nversion: 0.1.0\n"
	write("infra/helm/loopzero-api/Chart.yaml", chart)
	write("charts/loopzero-api/Chart.yaml", chart)

	// Bootstrap side: cluster-wide secrets, WITH delete.
	write("infra/helm/loopzero-api/templates/rbac.yaml", `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: loopzero-api
rules:
  - apiGroups: [""]
    resources: [secrets]
    verbs: [get, list, watch, create, update, patch, delete]
`)

	// Published side: identical but for `delete`.
	write("charts/loopzero-api/templates/rbac.yaml", `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: loopzero-api
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "watch", "create", "update", "patch"]
`)

	write("infra/quality/chart-pairs.txt", "loopzero-api | declared pair; both sides must grant the same\n")

	if _, err := chartpairs.Check(chartpairs.Config{RepoRoot: root}); err == nil {
		t.Fatal("declared pair diverges on exactly one cell -- (core)/secrets/delete, " +
			"granted by infra/helm and withheld by charts/ -- and the gate reported PASS. " +
			"The gate compares Chart.yaml names only; it never reads templates/, so no " +
			"divergence in what either side GRANTS can ever fail it.")
	}
}

// ---------------------------------------------------------------------------
// Real pair: the gate is silent about the divergence B-001 measured.
// ---------------------------------------------------------------------------

type rbacRule struct {
	APIGroups []string `yaml:"apiGroups"`
	Resources []string `yaml:"resources"`
	Verbs     []string `yaml:"verbs"`
}

type rbacDoc struct {
	Kind  string     `yaml:"kind"`
	Rules []rbacRule `yaml:"rules"`
}

// renderCells renders one chart and expands every ClusterRole rule into its
// (apiGroup, resource, verb) cells. It is the ORACLE: an independent computation of
// what each side grants, which the gate's own finding is then checked against.
func renderCells(t *testing.T, chartDir, showOnly string) map[string]bool {
	t.Helper()

	// NOT t.Skip. This test exists to measure the real charts, and a skip would report
	// green having measured nothing -- the precise failure class B-001 is about, and
	// the one `.claude/agents/cmd.md` names as this domain's highest severity. helm is
	// a declared prerequisite of this repository (CLAUDE.md, Helm v3.14+).
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatalf("helm is not on PATH and this test measures real rendered charts: %v", err)
	}

	out, err := exec.Command("helm", "template", "b001", chartDir, "--show-only", showOnly).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template %s --show-only %s: %v\n%s", chartDir, showOnly, err, out)
	}

	cells := map[string]bool{}
	dec := yaml.NewDecoder(strings.NewReader(string(out)))
	for {
		var doc rbacDoc
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
		t.Fatalf("no ClusterRole cells rendered from %s -- measuring nothing is not the "+
			"same as measuring zero divergence", chartDir)
	}
	return cells
}

// The gate's verdict on the REAL loopzero-api pair must agree with the oracle.
//
// Written as AGREEMENT rather than as "must always fail" on purpose. B-001's DoD (c)
// allows the pair to be resolved by CONVERGING it, and a test asserting unconditional
// failure would go green on that day without the gate having been fixed -- a test that
// silently stops testing. Agreement stays meaningful either way: diverging pair =>
// gate must report; converged pair => gate must stay quiet.
//
// FAILS on the current state: the oracle finds 162 divergent cells of a 200-cell union
// and `Check` returns nil.
func TestRealLoopZeroAPIPairDivergenceIsReported(t *testing.T) {
	skipUnlessRed(t)
	root := filepath.Join("..", "..", "..", "..", "..")

	boot := renderCells(t, filepath.Join(root, "infra/helm/loopzero-api"), "templates/serviceaccount.yaml")
	pub := renderCells(t, filepath.Join(root, "charts/loopzero-api"), "templates/clusterrole.yaml")

	var divergent []string
	agreeing := 0
	for c := range boot {
		if pub[c] {
			agreeing++
		} else {
			divergent = append(divergent, "bootstrap-only "+c)
		}
	}
	for c := range pub {
		if !boot[c] {
			divergent = append(divergent, "published-only "+c)
		}
	}
	sort.Strings(divergent)
	union := len(boot) + len(pub) - agreeing

	_, err := chartpairs.Check(chartpairs.Config{RepoRoot: root})

	switch {
	case len(divergent) > 0 && err == nil:
		sample := divergent
		if len(sample) > 8 {
			sample = sample[:8]
		}
		t.Fatalf("oracle: the declared loopzero-api pair diverges on %d of %d "+
			"(apiGroup, resource, verb) cells (%d agree); bootstrap grants %d, published %d.\n"+
			"first %d divergent cells:\n  %s\n"+
			"chartpairs.Check reported PASS. The gate never reads templates/, so it cannot "+
			"see any of this.",
			len(divergent), union, agreeing, len(boot), len(pub),
			len(sample), strings.Join(sample, "\n  "))
	case len(divergent) == 0 && err != nil:
		t.Fatalf("oracle: the loopzero-api pair grants identically (%d cells), "+
			"but the gate failed: %v", union, err)
	}
}
