// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package readinesssummary

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

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

// O mutante. Um persist de `.status` sem dobra nenhuma — nem em linha, nem por
// `defer` em quem chama. Sem esta prova, um verde do gate seria indistinguivel de
// um gate que nao enxerga a forma.
func TestReprovaPersistSemDobraNenhuma(t *testing.T) {
	raiz := t.TempDir()
	escrever(t, raiz, "operators/internal/x/c.go", `package x

func (r *R) reconcileAlgo() error {
	return r.Status().Patch(nil, nil, nil)
}
`)
	out, err := Check(Config{RepoRoot: raiz, Pacotes: []string{"operators/internal/x"}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("persist sem dobra passou — o gate nao enxerga a forma que existe para vigiar")
	}
	if len(out.Violacoes) != 1 {
		t.Fatalf("queria 1 violacao, veio %d: %+v", len(out.Violacoes), out.Violacoes)
	}
}

// A capacidade NOVA, e a razao deste gate existir: a dobra por `defer` acontece no
// RECONCILE, e o persist vive num helper que ele chama. A guarda antiga
// (`exposure_status_writer_test.go`) olha so' a funcao que envolve o `Patch`, entao
// contava esta forma como ausencia — foi assim que o `preview` mediu "0 de 6"
// tendo cobertura (B-138). `DeferReadinessSummary` re-busca o CR e PERSISTE o
// sumario no retorno, ou seja, e' a ultima escrita; o persist do helper esta'
// coberto.
func TestAceitaDobraPorDeferEmQuemChama(t *testing.T) {
	raiz := t.TempDir()
	escrever(t, raiz, "operators/internal/x/c.go", `package x

func (r *R) Reconcile() error {
	defer reconcileloop.DeferReadinessSummary(nil, nil, nil)()
	return r.falhar()
}

func (r *R) falhar() error {
	return r.Status().Patch(nil, nil, nil)
}
`)
	out, err := Check(Config{RepoRoot: raiz, Pacotes: []string{"operators/internal/x"}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("dobra por defer em quem chama foi reprovada; e' a forma canonica dos cinco reconcilers: %+v", out.Violacoes)
	}
}

// A dobra em linha cobre APENAS a propria funcao — ela e' uma mutacao antes da
// escrita daquela funcao, e nao diz nada sobre o que outra funcao escreve.
func TestDobraEmLinhaNaoCobreOutraFuncao(t *testing.T) {
	raiz := t.TempDir()
	escrever(t, raiz, "operators/internal/x/c.go", `package x

func (r *R) escreveDobrando() error {
	SetReadinessSummaryViaPkg(nil, nil)
	return r.Status().Patch(nil, nil, nil)
}

func (r *R) escreveSemDobrar() error {
	return r.Status().Update(nil, nil)
}
`)
	out, err := Check(Config{RepoRoot: raiz, Pacotes: []string{"operators/internal/x"}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("a dobra em linha de uma funcao cobriu o persist de OUTRA — isso nao e' o invariante")
	}
	if len(out.Violacoes) != 1 || !strings.Contains(out.Violacoes[0].Funcao, "escreveSemDobrar") {
		t.Errorf("a violacao deveria ser escreveSemDobrar: %+v", out.Violacoes)
	}
}

// A populacao real: os seis pacotes de operators. Um mutante prova que o detector
// dispara; so' isto diz o que ele encontra no que existe (`rules/testing.md` § 4.2).
// O resultado fica registrado aqui mesmo, inclusive se for zero.
func TestPopulacaoRealDosSeisPacotes(t *testing.T) {
	out, err := Check(Config{RepoRoot: qtestutil.Root(t)})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pacotes < 6 {
		t.Fatalf("varreu %d pacote(s); a populacao declarada do B-138 sao 6", out.Pacotes)
	}
	if out.Sitios == 0 {
		t.Fatal("zero persists encontrados — verde sobre nada nao e' verde")
	}
	t.Logf("populacao: %d pacotes, %d persists, %d cobertos, %d violacao(oes)",
		out.Pacotes, out.Sitios, out.Sitios-len(out.Violacoes), len(out.Violacoes))
	for _, v := range out.Violacoes {
		t.Logf("  descoberto: %s:%d %s (%s)", v.Arquivo, v.Linha, v.Funcao, v.Forma)
	}
}

// Dois tipos no MESMO pacote com metodo de mesmo nome. E' a forma real de
// `operators/internal/app`, que tem dois `Reconcile` — Application e Exposure — e
// so' o Exposure carrega o `defer`. Se o grafo indexar por NOME, o defer de um
// cobre os helpers do outro e o gate reporta verde sobre um persist descoberto.
func TestNaoConfundeMetodosHomonimosDeTiposDiferentes(t *testing.T) {
	raiz := t.TempDir()
	escrever(t, raiz, "operators/internal/x/c.go", `package x

func (r *ComDefer) Reconcile() error {
	defer reconcileloop.DeferReadinessSummary(nil, nil, nil)()
	return r.persistir()
}

func (r *ComDefer) persistir() error {
	return r.Status().Patch(nil, nil, nil)
}

func (r *SemDefer) Reconcile() error {
	return r.gravar()
}

func (r *SemDefer) gravar() error {
	return r.Status().Update(nil, nil)
}
`)
	out, err := Check(Config{RepoRoot: raiz, Pacotes: []string{"operators/internal/x"}})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("o defer de um tipo cobriu o helper de OUTRO — o grafo esta' indexado por nome e vaza")
	}
	if len(out.Violacoes) != 1 || !strings.Contains(out.Violacoes[0].Funcao, "gravar") {
		t.Errorf("a violacao deveria ser SemDefer.gravar: %+v", out.Violacoes)
	}
}
