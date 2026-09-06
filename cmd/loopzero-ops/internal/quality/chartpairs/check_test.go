// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package chartpairs_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/chartpairs"
)

func tree(t *testing.T, infraHelm, charts map[string]string, registro string) string {
	t.Helper()
	root := t.TempDir()
	escrever := func(base, dir, nome string) {
		p := filepath.Join(root, base, dir, "Chart.yaml")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("apiVersion: v2\nname: "+nome+"\nversion: 0.1.0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for dir, nome := range infraHelm {
		escrever("infra/helm", dir, nome)
	}
	for dir, nome := range charts {
		escrever("charts", dir, nome)
	}
	p := filepath.Join(root, "infra/quality/chart-pairs.txt")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(registro), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// O registro tem de listar exatamente os pares que existem em disco.
//
// B-056, medido 2026-08-18: dois charts declaram o MESMO `name` em arvores com ciclos de
// vida distintos — `infra/helm/` (bootstrap, Helmfile L2) e `charts/` (OCI publicado,
// ArgoCD L3). O `CLAUDE.md` manda manter o par em paridade, e a unica forma de saber de
// qual se fala e pelo caminho.
func TestRegistryMatchesDisk(t *testing.T) {
	root := tree(t,
		map[string]string{"loopzero-api": "loopzero-api", "gitea": "gitea"},
		map[string]string{"loopzero-api": "loopzero-api", "so-em-charts": "so-em-charts"},
		"# registro\nloopzero-api | ambos servem a API; o de infra/helm faz bootstrap, o de charts e publicado\n")

	if _, err := chartpairs.Check(chartpairs.Config{RepoRoot: root}); err != nil {
		t.Fatalf("registro bate com o disco; veio %v", err)
	}
}

// Par novo sem declaracao e o caso que faz o contrato de paridade envelhecer em silencio.
func TestUndeclaredPairIsRefused(t *testing.T) {
	root := tree(t,
		map[string]string{"loopzero-api": "loopzero-api", "loopzero-cloud": "loopzero-cloud"},
		map[string]string{"loopzero-api": "loopzero-api", "loopzero-cloud": "loopzero-cloud"},
		"# registro\nloopzero-api | nota\n")

	out, err := chartpairs.Check(chartpairs.Config{RepoRoot: root})
	if !errors.Is(err, chartpairs.ErrUndeclared) {
		t.Fatalf("esperava ErrUndeclared, veio %v", err)
	}
	if len(out.Undeclared) != 1 || out.Undeclared[0] != "loopzero-cloud" {
		t.Errorf("esperava loopzero-cloud como nao declarado, veio %v", out.Undeclared)
	}
}

// Declaracao que sobreviveu ao par: e o defeito que o `CLAUDE.md` tinha — mandava aplicar o
// mesmo conserto num par que nao existe, porque `infra/helm/loopzero-admission-policies` nunca
// existiu. Diluir o contrato com um par inexistente enfraquece os que sao reais.
func TestStaleDeclarationIsRefused(t *testing.T) {
	root := tree(t,
		map[string]string{"loopzero-api": "loopzero-api"},
		map[string]string{"loopzero-api": "loopzero-api"},
		"# registro\nloopzero-api | nota\nloopzero-fantasma | par que nao existe\n")

	out, err := chartpairs.Check(chartpairs.Config{RepoRoot: root})
	if !errors.Is(err, chartpairs.ErrStale) {
		t.Fatalf("esperava ErrStale, veio %v", err)
	}
	if len(out.Stale) != 1 || out.Stale[0] != "loopzero-fantasma" {
		t.Errorf("esperava loopzero-fantasma como obsoleto, veio %v", out.Stale)
	}
}

// Zero pares em disco com registro nao vazio tambem e divergencia; e zero com zero e
// legitimo, mas nao pode ser confundido com "nao varri nada".
func TestNoChartsAtAllIsRefused(t *testing.T) {
	root := t.TempDir()
	if _, err := chartpairs.Check(chartpairs.Config{RepoRoot: root}); !errors.Is(err, chartpairs.ErrNothingScanned) {
		t.Fatalf("esperava ErrNothingScanned, veio %v", err)
	}
}

// A populacao real deste repositorio.
func TestRealTree(t *testing.T) {
	out, err := chartpairs.Check(chartpairs.Config{RepoRoot: filepath.Join("..", "..", "..", "..", "..")})
	if err != nil {
		t.Fatalf("arvore real diverge: %v (pares=%v nao-declarados=%v obsoletos=%v)",
			err, out.Pairs, out.Undeclared, out.Stale)
	}
	if len(out.Pairs) == 0 {
		t.Fatal("zero pares medidos — verde sobre nada")
	}
	t.Logf("populacao real: %d chart(s) em infra/helm, %d em charts/, pares: %v",
		out.InfraHelm, out.Charts, out.Pairs)
}
