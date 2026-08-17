// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testinventory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

var repoRoot = qtestutil.MustRepoRoot()

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A tag é declaração do autor e vence o caminho — que é inferência de quem escreveu
// o classificador.
func TestBuildTagBeatsPath(t *testing.T) {
	// Caminho diz unit; a tag diz live.
	cat, reason := Classify("api/internal/x/foo_test.go", "//go:build live\n\npackage x\n")
	if cat != E2E {
		t.Fatalf("a tag declarada pelo autor precisa vencer o caminho; got %s (%s)", cat, reason)
	}
}

// `live` não pode casar dentro de `livetest` — casamento por substring transformaria
// qualquer tag que contenha a outra num falso positivo silencioso.
func TestBuildTagMatchesWholeTokenOnly(t *testing.T) {
	if cat, _ := Classify("a/b_test.go", "//go:build livetest\n\npackage b\n"); cat == E2E {
		t.Fatal("`livetest` não é a tag `live`; o casamento tem de ser por token inteiro")
	}
}

func TestPathClassifiesWhenNoTag(t *testing.T) {
	cases := map[string]Category{
		"infra/tests/e2e/deploy_test.go":     E2E,
		"infra/tests/chaos/cell_test.go":     E2E,
		"infra/tests/charts/render_test.go":  Integration,
		"api/tests/repositories_test.go":     Integration,
		"api/internal/routes/health_test.go": Unit,
		"pkg/naming/naming_test.go":          Unit,
	}
	for path, want := range cases {
		if got, reason := Classify(path, "package x\n"); got != want {
			t.Errorf("%s → %s (%s); want %s", path, got, reason, want)
		}
	}
}

// A razão existe para que um número derivado possa ser conferido. Um inventário
// derivado sem procedência teria o mesmo defeito do declarado.
func TestEveryClassificationCarriesItsReason(t *testing.T) {
	for _, in := range []string{"//go:build live\npackage x\n", "package x\n"} {
		if _, reason := Classify("infra/tests/e2e/a_test.go", in); reason == "" {
			t.Error("toda classificação precisa dizer qual regra a produziu")
		}
	}
}

func TestDeriveCountsAndRatios(t *testing.T) {
	root := t.TempDir()
	write(t, root, "pkg/a/a_test.go", "package a\n")
	write(t, root, "pkg/b/b_test.go", "package b\n")
	write(t, root, "infra/tests/e2e/e_test.go", "package e\n")
	write(t, root, "api/tests/i_test.go", "package i\n")
	write(t, root, "pkg/a/a.go", "package a\n") // não é teste

	inv, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Total() != 4 {
		t.Fatalf("total=%d; um arquivo não-teste não entra no inventário", inv.Total())
	}
	if inv.Unit != 2 || inv.Integr != 1 || inv.E2E != 1 {
		t.Fatalf("unit=%d integr=%d e2e=%d", inv.Unit, inv.Integr, inv.E2E)
	}
	u, i, e := inv.Ratios()
	if u != 50 || i != 25 || e != 25 {
		t.Fatalf("razões derivadas erradas: %.1f/%.1f/%.1f", u, i, e)
	}
}

// Árvore vazia devolve zeros sem dividir por zero — e o consumidor decide o que
// fazer com isso, em vez de receber NaN.
func TestEmptyTreeIsZeroNotNaN(t *testing.T) {
	inv, err := Derive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if u, i, e := inv.Ratios(); u != 0 || i != 0 || e != 0 {
		t.Fatalf("árvore vazia deve dar 0/0/0; got %.1f/%.1f/%.1f", u, i, e)
	}
}

// `testdata/` e `node_modules/` guardam código de fixture ou de terceiro. Contá-los
// faria a pirâmide descrever os testes de outra pessoa.
func TestForeignAndFixtureTreesAreSkipped(t *testing.T) {
	root := t.TempDir()
	write(t, root, "pkg/a/a_test.go", "package a\n")
	write(t, root, "pkg/a/testdata/fixture_test.go", "package fixture\n")
	write(t, root, "web/node_modules/dep/d_test.go", "package d\n")

	inv, err := Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Total() != 1 {
		t.Fatalf("total=%d; fixture e dependência de terceiro não são a suíte deste repo: %+v", inv.Total(), inv.Entries)
	}
}

// A população real (B-109). O número que importa é que a derivação enxerga a árvore
// INTEIRA — o inventário declarado via 375 de 988, e era essa cegueira o defeito.
func TestRealTreeIsFullyEnumerated(t *testing.T) {
	inv, err := Derive(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Total() < 900 {
		t.Fatalf("total=%d — a árvore tem ~988 arquivos `_test.go`; um número muito abaixo significa que a varredura está cega, que é exatamente o defeito que o B-106 removeu", inv.Total())
	}
	if inv.E2E == 0 {
		t.Fatal("zero e2e na árvore real — `infra/tests/e2e/` existe, e o inventário declarado dizia 1 de 710 justamente por não a enxergar")
	}
}
