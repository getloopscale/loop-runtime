// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package trufflehogscan refuses to call clean what it could not verify.
//
// # O defeito que este pacote consertou, medido
//
// Até 2026-08-18 este gate tinha **três** caminhos terminando em `exit 0`, e dois deles
// não sabiam se havia segredo:
//
//	binário ausente        →  Skipped: true            →  exit 0
//	erro de execução       →  Skipped: true            →  exit 0   ← o comentário do
//	                          código dizia "(network? git access?)" e devolvia sucesso
//	exit 0 --only-verified →  Pass: "scan clean"       →  exit 0
//
// O terceiro é o que custou cinco meses. `--only-verified` pergunta *"algum candidato
// está vivo?"*, e essa pergunta **exige rede**. Sem rede nenhum candidato se verifica, a
// ferramenta sai 0, e o gate imprimia "scan clean (no secrets detected)". **A ausência de
// resposta era lida como resposta negativa** — e um token do DigitalOcean vivo atravessou
// 2026-03-22 → 2026-08-17 e vários pushes no mesmo dia (B-142, issue #323).
//
// A suíte de testes anterior não poderia ter pego nada: três testes, um deles rodando o
// gate real e descartando o resultado com `_ = got`.
//
// # Uma passada, dois fatos
//
// A saída `--json` carrega `Verified` **por achado**. Uma execução responde as duas
// perguntas — todos os achados são o CONJUNTO DE CANDIDATOS, os `Verified:true` são os
// VIVOS. O primeiro rascunho pedia duas passadas; a medição derrubou o desenho (~70s cada,
// e o que domina é o passeio pelos 5.420 commits, não a rede).
//
//	binário ausente                    →  FAIL
//	erro de execução (rede, git, prazo) →  FAIL — nunca mais SKIP → exit 0
//	qualquer achado Verified:true       →  FAIL — a linha de base NÃO suprime
//	candidato fora da linha de base     →  FAIL — novo e não adjudicado
//	tudo adjudicado, nada vivo          →  PASS
//
// O que muda: um verde significava *"nada se verificou como vivo"* — afirmação que a rede
// pode fabricar. Passa a significar *"o conjunto de candidatos é exatamente o que já foi
// adjudicado, e nada nele está vivo"*, e a primeira metade é **offline e determinística**.
// O token teria sido barrado no dia em que entrou: candidato novo. Em março, não em agosto.
//
// # A linha de base, e por que ela não é um bypass
//
// Um candidato novo **bloqueia** até alguém regravá-la, e a regravação aparece como diff
// no PR — é esse diff que é a adjudicação. Mesmo mecanismo da catraca do `testpyramid`
// (B-106) e das isenções do `publishhygiene` (B-130).
//
// Ela é gerada com `--no-verification`, e isso foi **medido**, não escolhido: esse modo
// produz superconjunto estrito do verificado (67 ⊇ 65; o inverso é vazio — dois candidatos
// `JDBC` que a verificação descarta). Gerar pelo superconjunto impede o modo de falha
// oposto: se a rede cair e a varredura verificada passar a enxergar os dois, eles já estão
// na base, e o gate não reprova por acidente de rede.
//
// # O residual, dito em voz alta
//
// Um candidato **já adjudicado** que se torne vivo só é pego quando a verificação roda.
// Não é hipótese fechada — é o que sobra deste desenho, e está escrito aqui em vez de ser
// descoberto por alguém depois.
//
// Decisão registrada em
// `.claude/knowledge-base/decisions/MEET-ASYNC-AMENDMENT-2026-08-18-secret-gate-cannot-call-unverifiable-clean.md`
// (T2, pendente de ratificação).
package trufflehogscan

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The four ways this gate refuses. Each is typed so the caller can never collapse
// "não consegui verificar" into "não há" — the whole defect this package closed.
var (
	// ErrLiveSecret means a candidate was confirmed live by its provider.
	ErrLiveSecret = errors.New("credencial VIVA no histórico")
	// ErrUnadjudicated means a candidate is absent from the baseline.
	ErrUnadjudicated = errors.New("candidato novo não adjudicado")
	// ErrScannerMissing means trufflehog is not installed.
	//
	// FAIL e não skip: uma suíte ausente aparece no relatório, mas um scanner de
	// segredos ausente produz exatamente o mesmo `exit 0` de um repositório limpo.
	// Mesmo precedente do `helmfile` em taskfiles/quality.yml.
	ErrScannerMissing = errors.New("trufflehog não encontrado — sem ele o verde não distingue limpo de não-varrido")
	// ErrScanFailed means the scan could not complete (network, git, timeout).
	ErrScanFailed = errors.New("varredura não completou")
	// ErrNothingScanned means the scan produced no candidate while the baseline holds some.
	//
	// Uma base com entradas e uma varredura com zero candidatos não é uma árvore que
	// ficou limpa — é uma varredura que não alcançou o histórico. Reportar verde aí é a
	// forma `PASS — 0 routes` que este repositório já pagou três vezes.
	ErrNothingScanned = errors.New("varredura não alcançou nada — não achar nada não é o mesmo que não achar nada errado")
)

// defaultTimeout — a varredura mede ~70s nesta árvore; o teto existe para que uma
// varredura pendurada reprove em vez de travar o gate de entrega para sempre (EC-4).
const defaultTimeout = 20 * time.Minute

// Scanner runs the secret scan. É a costura que tira rede e binário do caminho dos
// testes (DIP) — sem ela, provar o comportamento offline exigiria estar offline.
type Scanner interface {
	Scan(ctx context.Context, repoRoot string) (string, error)
}

// Config drives the check.
type Config struct {
	RepoRoot     string
	BaselinePath string
	Timeout      time.Duration
	// Scanner defaults to the real trufflehog process.
	Scanner Scanner
}

// Outcome reports what was established — never merely that nothing was seen.
type Outcome struct {
	Candidates    int
	Live          int
	NewCandidates []Finding
	StaleEntries  []Entry
	Reason        string
}

// Check runs the scan and applies the policy.
func Check(ctx context.Context, cfg Config) (Outcome, error) {
	var out Outcome

	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	basePath := cfg.BaselinePath
	if basePath == "" {
		basePath = filepath.Join(root, defaultBaselineRel)
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	scanner := cfg.Scanner
	if scanner == nil {
		scanner = execScanner{}
	}

	// A base é lida ANTES da varredura: sem o conjunto adjudicado não há verde possível,
	// e gastar 70s para depois descobrir isso é desperdício.
	base, err := LoadBaseline(basePath)
	if err != nil {
		return out, err
	}

	scanCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	raw, err := scanner.Scan(scanCtx, root)
	if err != nil {
		// A garantia é DAQUI, não da implementação: qualquer Scanner que devolva erro
		// cru — rede, prazo, git — vira ErrScanFailed. Deixar isso a cargo de cada
		// implementação é como o skip voltaria a existir sem ninguém notar.
		if errors.Is(err, ErrScannerMissing) || errors.Is(err, ErrScanFailed) {
			return out, err
		}
		return out, fmt.Errorf("%w: %w", ErrScanFailed, err)
	}
	findings, err := ParseFindings(strings.NewReader(raw))
	if err != nil {
		return out, err
	}

	out.Candidates = len(findings)
	for _, f := range findings {
		if f.Live {
			out.Live++
		}
	}
	out.NewCandidates = base.Unadjudicated(findings)
	out.StaleEntries = base.Stale(findings)

	// Antes de qualquer veredito de conteúdo: a varredura chegou ao histórico?
	// Se a árvore FOI mesmo limpa, o conserto é regravar a base — um humano confirmando
	// "sim, tudo saiu". O gate não decide isso sozinho, em silêncio.
	if len(base.Entries) > 0 && out.Candidates == 0 {
		return out, fmt.Errorf("%w: a linha de base tem %d entrada(s) e a varredura devolveu zero candidato(s)",
			ErrNothingScanned, len(base.Entries))
	}

	// Ordem deliberada: o vivo reprova ANTES do não-adjudicado, porque é o defeito mais
	// grave e porque a base não pode suprimí-lo (ADR-3).
	if out.Live > 0 {
		var names []string
		for _, f := range findings {
			if f.Live {
				names = append(names, f.String())
			}
		}
		return out, fmt.Errorf("%w: %s", ErrLiveSecret, strings.Join(names, "; "))
	}
	if len(out.NewCandidates) > 0 {
		var names []string
		for _, f := range out.NewCandidates {
			names = append(names, f.String())
		}
		return out, fmt.Errorf("%w (%d): %s — remova o segredo, ou adjudique com `--write` (que RECUSA enquanto houver vivo)",
			ErrUnadjudicated, len(out.NewCandidates), strings.Join(names, "; "))
	}

	// A razão diz o que foi ESTABELECIDO. "scan clean" é a frase que sustentou cinco
	// meses de exposição: ela não distingue "não há" de "não verifiquei".
	out.Reason = fmt.Sprintf("%d candidato(s) varrido(s), todos adjudicados; %d verificado(s) como vivo(s); %d entrada(s) da linha de base sem achado nesta passada",
		out.Candidates, out.Live, len(out.StaleEntries))
	return out, nil
}

// execScanner runs the real trufflehog process.
type execScanner struct {
	binary string
	// noVerification drives baseline generation — o superconjunto medido.
	noVerification bool
}

func (e execScanner) Scan(ctx context.Context, root string) (string, error) {
	bin := e.binary
	if bin == "" {
		bin = "trufflehog"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrScannerMissing, err)
	}

	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrScanFailed, err)
	}
	args := []string{"git", "file://" + abs, "--json", "--no-update"}
	if e.noVerification {
		args = append(args, "--no-verification")
	}

	// #nosec G204 -- `path` vem de exec.LookPath sobre um nome de binário do próprio
	// gate ("trufflehog"), sobrescrito apenas por teste; `args` são literais e a raiz
	// do repositório já absolutizada. Nada aqui é entrada de usuário.
	cmd := exec.CommandContext(ctx, path, args...)
	stdout, runErr := cmd.Output()
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) && isScanRan(exitErr.ExitCode()) {
			return string(stdout), nil
		}
		return "", fmt.Errorf("%w: %w", ErrScanFailed, runErr)
	}
	return string(stdout), nil
}

// isScanRan reports whether an exit code means the scan completed.
//
// Com `--fail` o trufflehog sai 183 quando acha algo; sem, sai 0. Este gate não passa
// `--fail` (decide pelo JSON), mas aceita os dois para não depender de qual comportamento
// a versão instalada tem (EC-7).
func isScanRan(code int) bool { return code == 0 || code == 183 }

// Generate (re)writes the adjudicated baseline.
//
// Roda com `--no-verification` DE PROPÓSITO: esse modo é superconjunto estrito do
// verificado (medido 67 ⊇ 65), e gerar pelo superconjunto é o que impede um vermelho
// falso quando a rede cai. Gerar pelo conjunto menor produziria exatamente isso — e uma
// trava que reprova por acidente de rede é desligada.
//
// A recusa quando há achado vivo mora em WriteBaseline, onde ela é indesviável.
func Generate(ctx context.Context, cfg Config) (int, error) {
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	basePath := cfg.BaselinePath
	if basePath == "" {
		basePath = filepath.Join(root, defaultBaselineRel)
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	scanner := cfg.Scanner
	if scanner == nil {
		scanner = execScanner{noVerification: true}
	}

	scanCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	raw, err := scanner.Scan(scanCtx, root)
	if err != nil {
		if errors.Is(err, ErrScannerMissing) || errors.Is(err, ErrScanFailed) {
			return 0, err
		}
		return 0, fmt.Errorf("%w: %w", ErrScanFailed, err)
	}
	findings, err := ParseFindings(strings.NewReader(raw))
	if err != nil {
		return 0, err
	}
	if err := WriteBaseline(basePath, findings); err != nil {
		return 0, err
	}
	b, err := LoadBaseline(basePath)
	if err != nil {
		return 0, err
	}
	return len(b.Entries), nil
}
