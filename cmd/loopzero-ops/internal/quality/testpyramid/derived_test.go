// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testpyramid

import (
	"errors"
	"strings"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testinventory"
)

var repoRoot = qtestutil.MustRepoRoot()

// treeWith monta uma árvore com a contagem pedida de cada tipo.
func treeWith(t *testing.T, unit, integ, e2e int) string {
	t.Helper()
	root := t.TempDir()
	mk := func(dir string, n int) {
		for k := 0; k < n; k++ {
			p := filepath.Join(root, dir, "f"+string(rune('a'+k))+"_test.go")
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("package x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("pkg/u", unit)
	mk("api/tests", integ)
	mk("infra/tests/e2e", e2e)
	return root
}

func freeze(t *testing.T, root string) string {
	t.Helper()
	inv, err := testinventory.Derive(root)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "baseline.json")
	if err := WriteBaseline(p, inv, "2026-08-17", "teste"); err != nil {
		t.Fatal(err)
	}
	return p
}

// Ausência de linha de base é FALHA, nunca skip: sem ela a catraca não compara nada.
func TestMissingBaselineIsAFailure(t *testing.T) {
	root := treeWith(t, 8, 1, 1)
	_, err := CheckDerived(root, filepath.Join(root, "nao-existe.json"), DefaultThresholds())
	if !errors.Is(err, ErrBaselineMissing) {
		t.Fatalf("linha de base ausente precisa reprovar alto; err=%v", err)
	}
}

// A árvore congelada passa contra si mesma — sem isto a catraca nasceria vermelha.
func TestFrozenTreePassesAgainstItself(t *testing.T) {
	root := treeWith(t, 8, 1, 1)
	if _, err := CheckDerived(root, freeze(t, root), DefaultThresholds()); err != nil {
		t.Fatalf("a árvore medida deve passar contra a própria linha de base: %v", err)
	}
}

// Mais e2e que a linha de base é regressão, e regressão bloqueia.
func TestMoreE2EThanBaselineRegresses(t *testing.T) {
	root := treeWith(t, 8, 1, 1)
	base := freeze(t, root)
	// Acrescenta e2e depois de congelar.
	for _, n := range []string{"z1", "z2", "z3"} {
		p := filepath.Join(root, "infra/tests/e2e", n+"_test.go")
		if err := os.WriteFile(p, []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := CheckDerived(root, base, DefaultThresholds())
	if !errors.Is(err, ErrRegressed) {
		t.Fatalf("subir e2e acima da linha de base é regressão; err=%v out=%+v", err, out)
	}
	if len(out.Regressions) == 0 {
		t.Fatal("a regressão precisa ser nomeada, não só contada")
	}
}

// Melhorar não é regredir. Sem esta fronteira, a catraca puniria o conserto.
func TestImprovingIsNotARegression(t *testing.T) {
	root := treeWith(t, 8, 1, 1)
	base := freeze(t, root)
	for k := 0; k < 10; k++ {
		p := filepath.Join(root, "pkg/u", "extra"+string(rune('a'+k))+"_test.go")
		if err := os.WriteFile(p, []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := CheckDerived(root, base, DefaultThresholds()); err != nil {
		t.Fatalf("mais testes unitários melhora a pirâmide e não pode reprovar: %v", err)
	}
}

// A distância até o alvo do ADR é REPORTADA e não bloqueia — o alvo foi calibrado
// contra um arquivo que declarava 0,1% de e2e, isto é, contra ficção.
func TestAspirationGapIsReportedNotBlocking(t *testing.T) {
	root := treeWith(t, 5, 1, 4) // 50/10/40 — muito acima do alvo de e2e
	out, err := CheckDerived(root, freeze(t, root), DefaultThresholds())
	if err != nil {
		t.Fatalf("distância até o alvo do ADR não bloqueia: %v", err)
	}
	if len(out.AspirationGaps) == 0 {
		t.Fatal("a distância até o alvo precisa ser reportada — silenciá-la esconderia a decisão pendente")
	}
}

// A população real (B-109): a linha de base versionada tem de bater com a árvore que
// está no disco AGORA. É este teste que impede a linha de base de virar o próximo
// arquivo que descreve o passado.
func TestRealTreeMatchesTheFrozenBaseline(t *testing.T) {
	base := filepath.Join(repoRoot, "infra/quality/test-pyramid-baseline.json")
	out, err := CheckDerived(repoRoot, base, DefaultThresholds())
	if err != nil {
		t.Fatalf("a árvore real regrediu em relação à linha de base: %v (%+v)", err, out.Regressions)
	}
	if out.Total < 900 {
		t.Fatalf("total=%d — a derivação está enxergando menos que a árvore inteira", out.Total)
	}
}

// A distância especifica de e2e foi RATIFICADA pelo ADR de 2026-09-06 (Option A):
// para um produto cujo objeto É implantar, ~9% de e2e é cobertura genuína — a catraca
// é o mecanismo de enforcement, o alvo ≤5% do ADR D7 fica retirado para o eixo e2e.
// O aspiration gap de e2e passa a ser SILENCIADO; unit e integração continuam
// reportados (não foram amenizados).
func TestE2EAspirationGapIsSilencedAfterADRAmendment(t *testing.T) {
	root := treeWith(t, 5, 1, 4) // 50/10/40 — e2e muito acima do alvo D7
	out, err := CheckDerived(root, freeze(t, root), DefaultThresholds())
	if err != nil {
		t.Fatalf("catraca não deve reprovar a árvore congelada: %v", err)
	}
	for _, gap := range out.AspirationGaps {
		if strings.Contains(gap, "e2e") {
			t.Fatalf("gap de e2e devia estar silenciado pela emenda 2026-09-06 do ADR D7; achado: %q", gap)
		}
	}
	// Contra-teste: unit gap continua reportado (unit=50% < alvo 70%).
	foundUnit := false
	for _, gap := range out.AspirationGaps {
		if strings.Contains(gap, "unit") {
			foundUnit = true
			break
		}
	}
	if !foundUnit {
		t.Fatal("gap de unit precisa continuar reportado — a emenda cobre e2e, não a pirâmide inteira")
	}
}
