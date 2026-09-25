// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package admission_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// O `Allow` deste breaker nao tem chamador em producao, e o codigo nao dizia isso.
//
// B-054, medido 2026-08-18. O `api` constroi DOIS instrumentos sobre o mesmo pool Postgres,
// com a mesma politica declarada (rejeitar mutacao sob saturacao, deixar leitura passar):
//
//	api/internal/middleware/pgcircuitbreaker.go   portao percentual SEM histerese
//	api/internal/buildplane/admission/pg_breaker.go         high/low-water COM histerese
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
func TestOPortaoDoPoolEstaLigadoAoMiddleware(t *testing.T) {
	// A varredura por IMPORTADORES nao serve mais, e a razao e' o proprio conserto: o middleware
	// consulta este breaker por uma INTERFACE que ele mesmo declara (`middleware.PoolGate`), entao
	// nao importa este pacote. Um guarda que procurasse importadores continuaria reportando "sem
	// chamador" com o breaker ligado — verde sobre a pergunta errada.
	//
	// O que prova a ligacao e' a atribuicao: alguem constroi o breaker e o entrega como o portao
	// que o router consulta.
	raiz := raizDoRepositorio(t)

	fiacao, err := os.ReadFile(filepath.Join(raiz, "api", "cmd", "setup_server.go"))
	if err != nil {
		t.Fatalf("lendo a fiacao: %v", err)
	}
	f := string(fiacao)
	if !strings.Contains(f, "NewPGPoolBreaker(") {
		t.Error("`setup_server.go` nao constroi o PGPoolBreaker: sem ele o portao do pool volta a " +
			"ser o percentual instantaneo, que oscila na linha de corte (B-054)")
	}
	if !strings.Contains(f, "sc.Guards.PGPoolGate = ") {
		t.Error("o breaker e' construido mas nao e' entregue como `sc.Guards.PGPoolGate` — construido, " +
			"iniciado, parado no shutdown e nunca consultado foi exatamente o estado que o B-054 mediu")
	}

	router, err := os.ReadFile(filepath.Join(raiz, "api", "internal", "server", "router.go"))
	if err != nil {
		t.Fatalf("lendo o router: %v", err)
	}
	if !strings.Contains(string(router), "cfg.Guards.PGPoolGate") {
		t.Error("o router nao consulta `cfg.Guards.PGPoolGate`: o portao existe e ninguem pergunta a ele")
	}

	// E a advertencia de "sem chamador" tem de ter saido, senao o arquivo descreve no presente
	// um estado que deixou de existir — que e' a forma que este guarda nasceu para impedir.
	doc, err := os.ReadFile(filepath.Join(raiz, "api", "internal", "buildplane", "admission", "pg_breaker.go"))
	if err != nil {
		t.Fatalf("lendo o doc: %v", err)
	}
	if strings.Contains(string(doc), "NAO HA CHAMADOR") {
		t.Error("o breaker esta' ligado, mas o doc do arquivo ainda diz que nao ha chamador")
	}
}

// raizDoRepositorio sobe ate' o diretorio que contem `api/go.mod`. Independe de
// quantos niveis fundo o pacote esta', que e' exatamente a fragilidade que
// derrubou a versao anterior deste teste.
func raizDoRepositorio(t *testing.T) string {
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
	t.Fatal("nao achei a raiz do repositorio subindo a partir do cwd (procurei api/go.mod)")
	return ""
}

// caminhoDestePacote e' o sufixo de import que identifica este pacote nos
// importadores. Escrito UMA vez: quando o pacote desceu para `buildplane`
// (B-062) o filtro literal parou de casar, a populacao zerou e so' o piso
// abaixo impediu que a guarda ficasse verde sobre nada.
