// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package errorvocabulary

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
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
	t.Fatal("nao achei a raiz do repositorio (procurei api/go.mod)")
	return ""
}

func escrever(t *testing.T, raiz, rel, corpo string) {
	t.Helper()
	p := filepath.Join(raiz, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(corpo), 0o644); err != nil {
		t.Fatal(err)
	}
}

// O mutante: uma mensagem que nomeia a ferramenta interna. Sem esta prova o gate
// poderia estar verde por nao enxergar nada, que e' indistinguivel de nao haver
// defeito.
func TestReprovaMensagemComTermoBanido(t *testing.T) {
	raiz := t.TempDir()
	escrever(t, raiz, "api/internal/routes/x/x.go", `package x

type e struct{}

func (e) Wrap(string) e { return e{} }

var httpio struct {
	WriteLoopZeroError func(any, e)
}

func h() {
	httpio.WriteLoopZeroError(nil, e{}.Wrap("ArgoCD sync failed for tenant_id tn-1; check Gitea and MinIO"))
}
`)
	escrever(t, raiz, "api/internal/services/y/y.go", "package y\n")

	out, err := Check(Config{RepoRoot: raiz})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("mensagem com ArgoCD/tenant_id/Gitea/MinIO passou — o gate nao enxerga a forma que existe para vigiar")
	}
	if len(out.Violacoes) != 1 {
		t.Fatalf("queria 1 violacao (uma mensagem), veio %d: %+v", len(out.Violacoes), out.Violacoes)
	}
	if !strings.Contains(out.Violacoes[0].Mensagem, "ArgoCD") {
		t.Errorf("a violacao nao carrega a mensagem ofensora: %+v", out.Violacoes[0])
	}
}

// A contrapartida: um literal com o mesmo termo FORA de um escritor de erro nao
// e' saida ao usuario — e' log ou nome de campo interno. Sem esta assercao o gate
// viraria uma proibicao geral da palavra no codigo, reprovaria por toda parte e
// seria desligado na primeira sexta-feira.
func TestNaoReprovaTermoForaDeEscritorDeErro(t *testing.T) {
	raiz := t.TempDir()
	escrever(t, raiz, "api/internal/routes/x/x.go", `package x

import "log/slog"

func h() {
	slog.Warn("ArgoCD sync failed for tenant_id tn-1")
}
`)
	escrever(t, raiz, "api/internal/services/y/y.go", "package y\n")

	out, err := Check(Config{RepoRoot: raiz})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("um log com termo interno foi reprovado; a Regra 3 protege a SAIDA AO USUARIO, nao o vocabulario do codigo: %+v", out.Violacoes)
	}
}

// A populacao real. Um mutante prova que o detector dispara; so' isto diz o que
// ele encontra no que existe. O item registrou que nenhuma mensagem carrega termo
// banido hoje — o gate nasce verde, e este teste e' o que sustenta essa frase.
func TestArvoreRealEstaLimpa(t *testing.T) {
	out, err := Check(Config{RepoRoot: repoRoot(t)})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Arquivos == 0 || out.Mensagens == 0 {
		t.Fatalf("varredura vazia (%d arquivos, %d mensagens): verde sobre nada nao e' verde", out.Arquivos, out.Mensagens)
	}
	// Piso MEDIDO em 2026-08-19: 168 literais dentro de escritores de erro. A
	// primeira versao deste teste exigiu 200, numero que eu tirei do "277
	// mensagens" do B-081 — e 277 conta CALL SITES de `WriteLoopZeroError`, nao
	// literais. Duas metricas diferentes; repetir uma no lugar da outra e' como se
	// inventa um numero. Um valor menor que 168 significa que a varredura
	// encolheu (escritor renomeado, raiz movida) e o verde passou a valer menos
	// sem que nada avisasse.
	if out.Mensagens < 168 {
		t.Errorf("apenas %d mensagem(ns) alcancada(s); a arvore real tinha 168 em 2026-08-19", out.Mensagens)
	}
	if !out.Pass {
		t.Errorf("a arvore real carrega mensagem com termo banido pela Regra 3:\n%+v", out.Violacoes)
	}
}
