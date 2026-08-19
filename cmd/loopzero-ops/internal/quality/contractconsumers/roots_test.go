// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package contractconsumers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A raiz de consumidor que NAO EXISTE precisa reprovar alto.
//
// `git grep -- a b c` ignora em silencio os pathspecs que nao casam nada e devolve
// os matches dos que casam. Medido em 2026-08-19: das tres raizes declaradas,
// `cloud/dashboard/src/` e `cli/src/` **nao existem mais neste repositorio** — sairam
// para `../loopzero-cloud` e `../loopzero-cli` na decomposicao (M4 e M2). Sobra `api/internal/`.
//
// Isso e' pior do que parece, e e' por isso que o teste e' de existencia e nao de
// contagem: as duas raizes que sumiram eram as de TypeScript, e sao elas que carregam
// os consumidores de FIO — os que leem o campo pelo nome do JSON e que o compilador
// nao ve'. O que sobrou e' Go, onde `.<tag>` so' pode ser seletor de campo, e um campo
// removido **quebra a compilacao**. Ou seja, a unica raiz viva e' aquela cujos
// consumidores ja' tem outra guarda, e as que precisavam do gate sumiram.
//
// Enquanto isso for silencioso, o gate reporta PASS sobre uma populacao que nao existe
// — a forma exata do B-040.
func TestRaizDeConsumidorAusenteReprovaEmVezDeSumir(t *testing.T) {
	raiz := t.TempDir()
	// Um checkout git minimo: o Check chama `git` com -C nesta raiz.
	if err := os.MkdirAll(filepath.Join(raiz, "api", "internal"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := Check(Config{RepoRoot: raiz})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out == nil {
		t.Fatal("Check devolveu outcome nulo")
	}
	if len(out.MissingRoots) == 0 {
		t.Fatalf("nenhuma raiz ausente reportada, e a arvore de teste so' tem api/internal — "+
			"as outras duas raizes declaradas (%v) nao existem e o gate seguiu como se varresse tudo",
			out.ScanRoots)
	}
	for _, r := range out.MissingRoots {
		if strings.Contains(r, "api/internal") {
			t.Errorf("api/internal foi reportada como ausente e ela existe: %v", out.MissingRoots)
		}
	}
}

// A populacao real: as tres raizes declaradas, medidas contra ESTE repositorio.
// Um mutante prova que o detector dispara; so' isto diz o que ele encontra no que
// existe (`rules/testing.md` § 4.2). Resultado medido em 2026-08-19: **2 das 3
// ausentes**, e sao as duas de TypeScript.
func TestPopulacaoRealDasRaizesDeConsumidor(t *testing.T) {
	raiz := repoRootParaTeste(t)
	var ausentes []string
	for _, r := range consumerRoots {
		if _, err := os.Stat(filepath.Join(raiz, r)); err != nil {
			ausentes = append(ausentes, r)
		}
	}
	t.Logf("raizes declaradas: %d; ausentes na arvore real: %d %v",
		len(consumerRoots), len(ausentes), ausentes)
	if len(consumerRoots) == 0 {
		t.Fatal("nenhuma raiz declarada — verde sobre nada nao e' verde")
	}
	// Nao afirmo aqui que 2 ausentes seja o estado CORRETO — a decisao de para onde
	// apontar (repos irmaos, tarefa cross-repo, ou aposentar a metade de consumidor)
	// esta' no B-100 e e' de reuniao. O que este teste garante e' que o numero nao
	// mude sem que alguem veja.
	if len(ausentes) > 2 {
		t.Errorf("%d raizes ausentes; eram 2 em 2026-08-19 (%v). A cobertura encolheu.",
			len(ausentes), ausentes)
	}
}

func repoRootParaTeste(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}
	for i := 0; i < 12; i++ {
		if _, err := os.Stat(filepath.Join(dir, "api", "go.mod")); err == nil {
			return dir
		}
		pai := filepath.Dir(dir)
		if pai == dir {
			break
		}
		dir = pai
	}
	t.Fatal("nao achei a raiz do repositorio")
	return ""
}
