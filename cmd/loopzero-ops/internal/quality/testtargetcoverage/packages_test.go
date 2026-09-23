// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testtargetcoverage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

// A granularidade passou de módulo para pacote — B-114. `runsTests` perguntava
// "existe ALGUM `go test` neste módulo?" e nunca olhava o padrão, então
// `cd infra/tests && go test ./unit/...` marcava o módulo inteiro coberto enquanto
// `scripts/` (12), `config/` (6) e `performance/` (23) não rodavam em alvo nenhum.

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPatternCovers(t *testing.T) {
	cases := []struct {
		pattern, pkg string
		want         bool
	}{
		{"./...", "unit", true},
		{"./...", "a/b/c", true},
		{"./unit/...", "unit", true},
		{"./unit/...", "unit/sub", true},
		{"./unit/...", "scripts", false},
		// A fronteira que um prefixo cru erraria: `unitario` não está sob `unit`.
		{"./unit/...", "unitario", false},
		{"./unit", "unit", true},
		{"./unit", "unit/sub", false},
	}
	for _, c := range cases {
		if got := patternCovers(c.pattern, c.pkg); got != c.want {
			t.Errorf("patternCovers(%q, %q) = %v; want %v", c.pattern, c.pkg, got, c.want)
		}
	}
}

// Flags NÃO encerram a lista de padrões — elas vêm antes deles na forma real
// (`go test -race -coverprofile=X ./charts/... ./chaos/...`). O que encerra é um
// operador de shell. A primeira versão deste teste codificava a regra errada, e ela
// devolvia zero padrões exatamente para o alvo que cobre cinco subárvores.
func TestPatternsSurviveFlagsAndStopAtAShellOperator(t *testing.T) {
	got := patternsFor(`cd infra/tests && go test -race -count=1 -coverprofile="$OUT" ./charts/... ./chaos/... 2>&1 | tail -20`, "infra/tests")
	if len(got) != 2 || got[0] != "./charts/..." || got[1] != "./chaos/..." {
		t.Fatalf("os padrões vêm DEPOIS das flags e vão até o operador de shell; got %v", got)
	}
}

// E o bloco multi-linha, que é a forma em que `quality.yml` declara essas cinco.
func TestMultiLineBlockIsUnderstood(t *testing.T) {
	block := "      - |\n        set -e\n        cd infra/tests\n        # comentário\n" +
		"        go test -race -coverprofile=\"$OUT\" \\\n          ./charts/... ./chaos/... 2>&1 | tail -20\n"
	got := patternsFor(block, "infra/tests")
	if len(got) != 2 {
		t.Fatalf("um `cd` numa linha e o `go test` noutra continuam sendo o mesmo comando; got %v", got)
	}
}

// A forma exata que o B-092 expôs: módulo com `./sub/...` num alvo e outra subárvore
// sem alvo nenhum. É a bala 3 do DoD.
func TestModuleWithRestrictedPatternLeavesOtherSubtreeUncovered(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.work", "go 1.26\n")
	writeFile(t, root, "infra/tests/go.mod", "module x\n")
	writeFile(t, root, "infra/tests/unit/u_test.go", "package u\n")
	writeFile(t, root, "infra/tests/scripts/s_test.go", "package s\n")
	writeFile(t, root, "taskfiles/test.yml", "  cmds:\n    - cd infra/tests && go test ./unit/...\n")

	out, err := Check(Config{
		RepoRoot: root, Taskfiles: []string{"taskfiles/test.yml"},
		Exemptions: []Exemption{}, Now: time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrPackageUncovered) {
		t.Fatalf("uma subárvore sem alvo precisa reprovar mesmo com o módulo tendo outro alvo; err=%v out=%+v", err, out)
	}
	joined := strings.Join(out.UncoveredPackages, " ")
	if !strings.Contains(joined, "scripts") {
		t.Fatalf("a subárvore descoberta precisa ser NOMEADA; got %v", out.UncoveredPackages)
	}
	if strings.Contains(joined, "unit") {
		t.Fatalf("a subárvore coberta não pode aparecer como descoberta; got %v", out.UncoveredPackages)
	}
}

// `./...` cobre tudo — a esmagadora maioria dos módulos, e a fronteira que impede a
// mudança de virar uma regra que o repositório não satisfaz.
func TestWildcardTargetCoversEveryPackage(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.work", "go 1.26\n")
	writeFile(t, root, "m/go.mod", "module m\n")
	writeFile(t, root, "m/a/a_test.go", "package a\n")
	writeFile(t, root, "m/b/c/c_test.go", "package c\n")
	writeFile(t, root, "taskfiles/test.yml", "  cmds:\n    - cd m && go test ./...\n")

	if _, err := Check(Config{
		RepoRoot: root, Taskfiles: []string{"taskfiles/test.yml"},
		Exemptions: []Exemption{}, Now: time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("`go test ./...` cobre todo pacote do módulo: %v", err)
	}
}

// Pacote SEM teste não exige alvo. Exigir transformaria o gate num pedido de escrever
// teste para tudo, que é outra conversa — o que ele afirma é mais estreito: o que
// existe está sendo rodado.
func TestPackageWithoutTestsNeedsNoTarget(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.work", "go 1.26\n")
	writeFile(t, root, "m/go.mod", "module m\n")
	writeFile(t, root, "m/a/a_test.go", "package a\n")
	writeFile(t, root, "m/semteste/x.go", "package semteste\n")
	writeFile(t, root, "taskfiles/test.yml", "  cmds:\n    - cd m && go test ./a/...\n")

	if _, err := Check(Config{
		RepoRoot: root, Taskfiles: []string{"taskfiles/test.yml"},
		Exemptions: []Exemption{}, Now: time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("pacote sem teste não precisa de alvo: %v", err)
	}
}

// Bala 2 do DoD: subárvore que exige cluster entra por isenção NOMEADA com a razão —
// não por o padrão do módulo acidentalmente cobri-la.
func TestPackageExemptionRequiresAReason(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.work", "go 1.26\n")
	writeFile(t, root, "m/go.mod", "module m\n")
	writeFile(t, root, "m/e2e/e_test.go", "package e2e\n")
	writeFile(t, root, "taskfiles/test.yml", "  cmds:\n    - cd m && go test ./nada/...\n")

	base := Config{
		RepoRoot: root, Taskfiles: []string{"taskfiles/test.yml"},
		Now: time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC),
	}

	// Sem razão: recusa.
	semRazao := base
	semRazao.Exemptions = []Exemption{{Module: "m", Packages: []string{"e2e"}, Sunset: "2026-11-11"}}
	if _, err := Check(semRazao); !errors.Is(err, ErrExemptionWithoutReason) {
		t.Fatalf("isenção de pacote sem razão precisa recusar; err=%v", err)
	}

	// Com razão: passa.
	comRazao := base
	comRazao.Exemptions = []Exemption{{
		Module: "m", Packages: []string{"e2e"}, Sunset: "2026-11-11",
		Reason: "exige cluster em pé; roda pelo alvo de e2e, não pela suíte unitária",
	}}
	if _, err := Check(comRazao); err != nil {
		t.Fatalf("isenção de pacote com razão e sunset deve passar: %v", err)
	}
}

// A população real (B-109): a árvore que ship precisa satisfazer a granularidade nova,
// senão a mudança seria uma regra que o próprio repositório não cumpre.
func TestRealTreeSatisfiesPackageGranularity(t *testing.T) {
	out, err := Check(Config{RepoRoot: qtestutil.Root(t)})
	if err != nil {
		t.Fatalf("a árvore real precisa satisfazer a granularidade de pacote; err=%v\n  descobertos: %v", err, out.UncoveredPackages)
	}
	if len(out.Modules) == 0 {
		t.Fatal("zero módulos varridos — a varredura não verificou nada")
	}
}
