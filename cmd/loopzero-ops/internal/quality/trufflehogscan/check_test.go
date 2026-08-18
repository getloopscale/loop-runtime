// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package trufflehogscan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

var repoRoot = qtestutil.MustRepoRoot()

// stubScanner é a costura que tira rede e binário do caminho do teste (DIP).
type stubScanner struct {
	out string
	err error
}

func (s stubScanner) Scan(context.Context, string) (string, error) { return s.out, s.err }

func baseWith(t *testing.T, fs []Finding) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "b.json")
	if err := WriteBaseline(p, fs); err != nil {
		t.Fatal(err)
	}
	return p
}

func run(t *testing.T, sc Scanner, basePath string) (Outcome, error) {
	t.Helper()
	return Check(context.Background(), Config{
		RepoRoot: t.TempDir(), BaselinePath: basePath, Scanner: sc,
	})
}

// ADR-3. A adjudicação humana cobre "isto não é segredo" — nunca "este segredo pode viver aqui".
func TestAchadoVivoReprovaMesmoEstandoNaBase(t *testing.T) {
	dead := found(t, line("DigitalOceanToken", "x.ts", fakeSecret(), false))
	base := baseWith(t, dead) // mesma identidade: Verified não entra no hash
	live := line("DigitalOceanToken", "x.ts", fakeSecret(), true)

	out, err := run(t, stubScanner{out: live}, base)
	if !errors.Is(err, ErrLiveSecret) {
		t.Fatalf("achado vivo reprova mesmo adjudicado; err=%v", err)
	}
	if out.Live != 1 {
		t.Fatalf("o vivo precisa ser contado; got %+v", out)
	}
}

// O caso do B-142 — e teria sido barrado em MARÇO, não em agosto.
func TestCandidatoNovoReprova(t *testing.T) {
	base := baseWith(t, found(t, line("Postgres", "a.md", pgURI(), false)))
	out, err := run(t, stubScanner{out: strings.Join([]string{
		line("Postgres", "a.md", pgURI(), false),
		line("DigitalOceanToken", "infra/b.ts", fakeSecret(), false),
	}, "\n")}, base)

	if !errors.Is(err, ErrUnadjudicated) {
		t.Fatalf("candidato fora da base reprova; err=%v", err)
	}
	if len(out.NewCandidates) != 1 || out.NewCandidates[0].Detector != "DigitalOceanToken" {
		t.Fatalf("o novo precisa ser NOMEADO para ser adjudicável; got %+v", out.NewCandidates)
	}
}

// O caminho 2 medido: o código nomeava a rede como causa e devolvia exit 0.
func TestErroDeExecucaoReprovaENaoESkip(t *testing.T) {
	base := baseWith(t, found(t, line("Postgres", "a.md", "x", false)))
	_, err := run(t, stubScanner{err: errors.New("dial tcp: network is unreachable")}, base)
	if !errors.Is(err, ErrScanFailed) {
		t.Fatalf("erro de execução reprova; err=%v", err)
	}
	if errors.Is(err, ErrLiveSecret) {
		t.Fatal("erro de execução não é o mesmo defeito que segredo vivo")
	}
}

// O caminho 1: ausência do scanner produz o MESMO exit 0 de um repositório limpo.
func TestBinarioAusenteReprova(t *testing.T) {
	base := baseWith(t, found(t, line("Postgres", "a.md", "x", false)))
	_, err := Check(context.Background(), Config{
		RepoRoot: t.TempDir(), BaselinePath: base,
		Scanner: execScanner{binary: "trufflehog-que-nao-existe-" + t.Name()},
	})
	if !errors.Is(err, ErrScannerMissing) {
		t.Fatalf("binário ausente reprova; err=%v", err)
	}
}

func TestConjuntoAdjudicadoSemVivosPassa(t *testing.T) {
	fs := found(t,
		line("Postgres", "a.md", pgURI(), false),
		line("URI", "b.md", credURI(), false))
	base := baseWith(t, fs)

	out, err := run(t, stubScanner{out: strings.Join([]string{
		line("Postgres", "a.md", pgURI(), false),
		line("URI", "b.md", credURI(), false),
	}, "\n")}, base)
	if err != nil {
		t.Fatalf("conjunto adjudicado sem vivos passa: %v (%+v)", err, out)
	}
	if out.Candidates != 2 || out.Live != 0 {
		t.Fatalf("contagens erradas: %+v", out)
	}
}

// O verde precisa dizer O QUE FOI ESTABELECIDO. "scan clean" é a frase que sustentou
// cinco meses de exposição: ela não distingue "não há" de "não verifiquei".
func TestOVerdeDizOQueFoiEstabelecido(t *testing.T) {
	fs := found(t, line("Postgres", "a.md", pgURI(), false))
	out, err := run(t, stubScanner{out: line("Postgres", "a.md", pgURI(), false)}, baseWith(t, fs))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.Reason, "clean") {
		t.Fatalf("a razão não pode reciclar a frase que escondeu o defeito: %q", out.Reason)
	}
	if !strings.Contains(out.Reason, "1") {
		t.Fatalf("a razão precisa citar o que foi medido: %q", out.Reason)
	}
}

// EC-4. Varredura pendurada trava o gate de entrega para sempre.
func TestPrazoExpiradoReprova(t *testing.T) {
	base := baseWith(t, found(t, line("Postgres", "a.md", "x", false)))
	_, err := Check(context.Background(), Config{
		RepoRoot: t.TempDir(), BaselinePath: base, Timeout: time.Nanosecond,
		Scanner: sleepyScanner{},
	})
	if !errors.Is(err, ErrScanFailed) {
		t.Fatalf("prazo expirado reprova, nunca skip; err=%v", err)
	}
}

type sleepyScanner struct{}

func (sleepyScanner) Scan(ctx context.Context, _ string) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

// EC-7. Com --fail o trufflehog sai 183; sem, sai 0. O gate não pode depender de qual
// comportamento a versão instalada tem.
func TestSaida0E183SaoExecucaoValida(t *testing.T) {
	for _, code := range []int{0, 183} {
		if !isScanRan(code) {
			t.Fatalf("código %d é execução válida", code)
		}
	}
	for _, code := range []int{1, 2, 127} {
		if isScanRan(code) {
			t.Fatalf("código %d é erro de execução", code)
		}
	}
}

// EC-2 no nível do gate: sem base, não há verde possível.
func TestSemBaseNaoHaVerde(t *testing.T) {
	_, err := run(t, stubScanner{out: ""}, filepath.Join(t.TempDir(), "ausente.json"))
	if !errors.Is(err, ErrBaselineMissing) {
		t.Fatalf("sem base o gate não pode reportar limpo; err=%v", err)
	}
}

// A população real (rules/testing.md § 4.2). O mutante sintético acima prova que a trava
// ENXERGA cada forma; só isto prova o que ela vê no que existe — e é a asserção que a
// suíte anterior não tinha (três testes, `_ = got`, zero asserções de comportamento).
func TestArvoreRealEstaAdjudicada(t *testing.T) {
	// Sem guarda de -short: nada no cmd/loopzero-ops roda com essa flag, e a varredura da
	// população é justamente a asserção que não pode ser pulada (rules/testing.md § 4.2).
	// Custa ~70s, que é o preço de o gate nascer medido contra a árvore que ship.
	if _, err := os.Stat(filepath.Join(repoRoot, defaultBaselineRel)); err != nil {
		t.Fatalf("a linha de base precisa estar versionada: %v", err)
	}
	out, err := Check(context.Background(), Config{RepoRoot: repoRoot})
	if err != nil {
		for i, f := range out.NewCandidates {
			if i == 10 {
				t.Errorf("... e mais %d", len(out.NewCandidates)-10)
				break
			}
			t.Errorf("novo: %s [%s] %s", f.File, f.Detector, f.ID())
		}
		t.Fatalf("árvore real: %v", err)
	}
	if out.Candidates == 0 {
		t.Fatal("zero candidatos varridos — não achar nada não é o mesmo que não achar nada errado")
	}
}

// Achado da fase REVIEW desta própria fatia. Uma base com 39 entradas e uma varredura
// com ZERO candidatos não é um repositório que ficou limpo — é uma varredura que não
// alcançou o histórico. Reportar verde aí é a forma exata do defeito que o B-143 mede,
// reintroduzida pela porta dos fundos.
//
// A direção do erro é deliberada: se a árvore FOI mesmo limpa, o conserto é regravar a
// base (que passa a ter zero entradas) — um humano confirmando "sim, tudo saiu". O
// contrário seria o gate decidir isso sozinho, em silêncio.
func TestBaseNaoVaziaComVarreduraVaziaReprova(t *testing.T) {
	base := baseWith(t, found(t, line("Postgres", "a.md", pgURI(), false)))
	_, err := run(t, stubScanner{out: ""}, base)
	if !errors.Is(err, ErrNothingScanned) {
		t.Fatalf("varredura que não alcançou nada não pode reportar limpo; err=%v", err)
	}
}

// A fronteira: base vazia E varredura vazia é um repositório genuinamente limpo.
func TestBaseVaziaComVarreduraVaziaPassa(t *testing.T) {
	base := baseWith(t, nil)
	if _, err := run(t, stubScanner{out: ""}, base); err != nil {
		t.Fatalf("repositório sem candidato nenhum passa: %v", err)
	}
}
