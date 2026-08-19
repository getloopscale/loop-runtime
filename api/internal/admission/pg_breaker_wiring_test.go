// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package admission_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// O `Allow` deste breaker nao tem chamador em producao, e o codigo nao dizia isso.
//
// B-054, medido 2026-08-18. O `api` constroi DOIS instrumentos sobre o mesmo pool Postgres,
// com a mesma politica declarada (rejeitar mutacao sob saturacao, deixar leitura passar):
//
//	api/internal/middleware/pgcircuitbreaker.go   portao percentual SEM histerese
//	api/internal/admission/pg_breaker.go         high/low-water COM histerese
//
// E a histerese nao e enfeite: o proprio arquivo do admission explica que sem ela o breaker
// oscila a cada tick perto da linha de corte.
//
// O que a medicao encontrou vai alem de "dois divergem". Em `api/cmd/main.go` o breaker do
// admission e construido (`:509`), iniciado (`:510`) e parado no shutdown (`:514`) — e
// **`Allow()` nunca e chamado**. Ele mede a saturacao, dispara com histerese e registra em
// log; ninguem lhe pergunta se deve permitir. Quem de fato barra requisicao e o outro, o
// que NAO tem histerese.
//
// Este teste trava a descoberta: enquanto `Allow` nao tiver chamador de producao, o codigo
// tem de DIZER isso onde alguem leia, em vez de descrever um "usage pattern" que ninguem
// segue.
func TestAllowHasNoProductionCallerAndTheCodeSaysSo(t *testing.T) {
	raiz := filepath.Join("..", "..", "..")

	chamadores, examinados := caçarChamadoresDeAllow(t, filepath.Join(raiz, "api"))

	// Sem esta assercao a guarda fica verde para sempre se o filtro esvaziar a populacao —
	// alguem renomeia o pacote, move a arvore ou muda o path do modulo, todo arquivo e
	// descartado, `chamadores` volta vazio e o teste entra no ramo "nao tem chamador" com a
	// advertencia congelada mesmo depois de alguem ligar o breaker. E a forma
	// `PASS — 0 rotas` que `rules/testing.md § 4.2` nomeia.
	//
	// Hoje sao exatamente dois: `api/cmd/main.go` e `api/cmd/adapters.go`.
	if examinados < 2 {
		t.Fatalf("a varredura examinou %d arquivo(s) que importam o pacote; o filtro "+
			"esvaziou a populacao e a guarda nao mede mais nada", examinados)
	}
	t.Logf("populacao real: %d arquivo(s) importam internal/admission, %d chamam Allow()",
		examinados, len(chamadores))
	doc := lerArquivo(t, filepath.Join(raiz, "api", "internal", "admission", "pg_breaker.go"))

	if len(chamadores) > 0 {
		// Alguem ligou o breaker: a advertencia deixou de valer e sai junto.
		if strings.Contains(doc, "NAO HA CHAMADOR") {
			t.Errorf("o `Allow` passou a ter chamador (%v) — a advertencia no topo do "+
				"arquivo ficou obsoleta e precisa sair", chamadores)
		}
		return
	}
	if !strings.Contains(doc, "NAO HA CHAMADOR") {
		t.Error("`Allow` nao tem chamador de producao e o arquivo nao avisa; quem lê o " +
			"\"usage pattern\" acredita que a protecao esta ligada")
	}
}

// caçarChamadoresDeAllow procura consumo do `Allow()` DESTE breaker no modulo api.
//
// Exigir que o arquivo importe `internal/admission` e o que separa o `Allow` do breaker do
// `Allow` dos limitadores de taxa, que tem o mesmo nome e chamadores legitimos. Sem esse
// filtro a primeira versao deste teste casava `rate_limit_vault.go` e passava pelo ramo
// "tem chamador" — verde pelo motivo errado, sobre o defeito que ele existe para travar.
//
// E textual, e isso esta dito: um chamador que receba o breaker por interface declarada em
// outro pacote escaparia. A alternativa seria analise de tipos, que custa mais do que este
// achado justifica hoje. (A revisao mediu que hoje nenhuma interface no repositorio declara
// `Allow() bool`, entao a evasao e possivel em tese e inexistente na pratica; e que o alias
// de import NAO escapa, porque o filtro casa o path e nao o identificador.)
//
// Varrer so `api/` NAO e estreiteza, e prova: `internal/` torna o pacote inalcancavel de
// fora do modulo por regra da linguagem, entao a varredura e completa por construcao.
func caçarChamadoresDeAllow(t *testing.T, raizAPI string) (chamadores []string, examinados int) {
	t.Helper()
	re := regexp.MustCompile(`\bAllow\(\s*\)`)
	err := filepath.Walk(raizAPI, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		if strings.Contains(p, filepath.Join("internal", "admission")) {
			return nil // o proprio pacote: definicao e doc, nao consumo
		}
		b, err := os.ReadFile(p) // #nosec G304 -- caminho vem do Walk sobre a arvore do repo
		if err != nil {
			return err
		}
		if !strings.Contains(string(b), "internal/admission") {
			return nil // nao importa o pacote: qualquer `Allow()` aqui e de outro tipo
		}
		examinados++
		for _, linha := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(linha), "//") {
				continue
			}
			if re.MatchString(linha) {
				chamadores = append(chamadores, p)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("varrer o modulo api: %v", err)
	}
	return chamadores, examinados
}

func lerArquivo(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		t.Fatalf("ler %s: %v", p, err)
	}
	return string(b)
}
