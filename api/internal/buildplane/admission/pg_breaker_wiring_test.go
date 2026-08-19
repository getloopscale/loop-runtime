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
	if !strings.Contains(f, "sc.PGPoolGate = ") {
		t.Error("o breaker e' construido mas nao e' entregue como `sc.PGPoolGate` — construido, " +
			"iniciado, parado no shutdown e nunca consultado foi exatamente o estado que o B-054 mediu")
	}

	router, err := os.ReadFile(filepath.Join(raiz, "api", "internal", "server", "router.go"))
	if err != nil {
		t.Fatalf("lendo o router: %v", err)
	}
	if !strings.Contains(string(router), "cfg.PGPoolGate") {
		t.Error("o router nao consulta `cfg.PGPoolGate`: o portao existe e ninguem pergunta a ele")
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
		if !strings.Contains(string(b), caminhoDestePacote) {
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
const caminhoDestePacote = "internal/buildplane/admission"
