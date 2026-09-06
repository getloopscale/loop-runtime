// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package publishhygiene refuses a versioned file that carries a real host address
// or a workstation path.
//
// # Por que aqui, e não só no loopzero-rag
//
// Dos **20** repositórios irmãos medidos em 2026-08-17, **um** tinha esta trava —
// `loopzero-rag/scripts/check-publish-hygiene.sh` — e foi por isso que o vazamento
// apareceu lá: ele reprovou `develop` por uma linha vinda do kit. Nos outros 19 o
// mesmo vazamento envelhecia em silêncio.
//
// Este repositório publica charts OCI e imagens, então é um dos lugares onde a
// publicação é real. E **história publicada é pública RETROATIVAMENTE** — diferente de
// quase toda dívida técnica, esta não se desfaz.
//
// # O critério é CAMINHO DE HOME, e a metade de IP do loopzero-rag NÃO foi transportada
//
// **Caminho**: apenas home de usuário REAL. `/home/{runner,node,dev,…}` são CI ou
// sintéticos e passam — é essa fronteira que separa esta trava de uma que reprova o
// próprio teste de segurança.
//
// A metade de IP ficou de fora, e a razão é medição, não preguiça. Rodada contra esta
// árvore ela produziu **104 achados, quase todos falsos**:
//
//	`13.3.2.1-1`, `15.5-10`   strings de VERSÃO casando com o formato de quatro octetos
//	`1.2.3.4`                 o placeholder canônico dos testes de DNS/CF
//	`173.245.48.0/20`         faixas CIDR da Cloudflare, que o código carrega de
//	                          propósito para reconhecer proxy confiável
//	`paulo@165.227.121.20`    o alvo vivo DECLARADO em `rules/live-target.txt`, que
//	                          `cycle-discover.md` exige para o modo live-test
//
// Fechá-los um a um seria uma corrida armamentista em que cada exceção enfraquece o
// sinal, e a última linha nem é um vazamento: é um contrato. A metade de IP fica onde
// foi calibrada — no `loopzero-rag`, cujo perfil é outro. Prometer cobertura de IP aqui
// seria a mesma superestimação que este ecossistema mede em três itens desta sessão.
//
// # E o que ele NÃO varre, que o loopzero-rag varre
//
// A trilha de auditoria. `docs/README.md` declara que `docs/wiki/`, `plans/completed/`,
// `operations/baselines/`, `meetings/records/`, `operations/findings/` e `reviews/`
// **nunca devem ser editados para corresponder ao presente** — reescrever um registro
// datado destrói a única evidência do que era verdade então.
//
// Isso não é conveniência: medido neste repositório, **738 das 796** ocorrências de
// caminho de home estão lá, e **495 num par de artefatos JUnit**. Varrer tudo produziria
// uma trava que só pode ficar verde violando outra regra do repositório — e uma trava
// que não pode ficar verde é desligada.
//
// A consequência honesta é que este gate protege o que se **escreve daqui para a
// frente**, não o que já foi registrado. Dizê-lo é a alternativa a um verde que se
// superestima.
package publishhygiene

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrLeak means a versioned live file carries a host address or workstation path.
var ErrLeak = errors.New("um arquivo versionado carrega caminho de workstation — história publicada é pública retroativamente")

// ErrNothingScanned means the scan reached no file.
//
// Ausência é FALHA, nunca skip: uma varredura que não alcançou nada reportando verde é
// a forma `PASS — 0 routes` que este repositório já pagou três vezes.
var ErrNothingScanned = errors.New("nenhum arquivo varrido — não achar nada não é o mesmo que não achar nada errado")

var (
	// homeRE finds a user home path; syntheticHomeRE removes CI, container and
	// fixture users.
	homeRE = regexp.MustCompile(`/(home|Users)/[a-z][a-z0-9_-]*/`)
	// `k6` entrou por medição: é o usuário DENTRO da imagem do k6
	// (`mountPath: /home/k6/scripts`), não a estação de ninguém.
	syntheticHomeRE = regexp.MustCompile(`/(home|Users)/(runner|node|dev|user|ubuntu|app|vscode|test|x|k6)/`)
	// urlHomeRE descarta `/home/` que é SEGMENTO DE URL, não caminho de disco —
	// `https://exemplo.com/home/produto` casava com a regra e não é vazamento de
	// nada. Medido em `landscape.yml`, que cataloga centenas de projetos externos.
	urlHomeRE = regexp.MustCompile(`https?://[^\s"'` + "`" + `]*/home/`)
)

// auditTrailPrefixes are the dated records `docs/README.md` forbids editing.
var auditTrailPrefixes = []string{
	"docs/wiki/",
	"docs/plans/completed/",
	"docs/operations/baselines/",
	"docs/operations/evidence/",
	"docs/operations/findings/",
	"docs/meetings/",
	"docs/reviews/",
	"docs/knowledge-base/",
	"docs/scripts-audit/",
	"docs/handoff/",
	".claude/knowledge-base/",
	".claude/agents/",
	".claude/.compaction-snapshots/",
	// `.claude/records/` is the canonical cycle-artifact tree (`rules/records-location.md`):
	// alignment briefs, plans, edge-case reviews, deps-audit reports, implementation logs,
	// mini-reviews, maintenance-runs, and cycle events. Every one of these is DATED evidence
	// that the docs/README convention explicitly forbids editing to match the present —
	// which is exactly what a workstation-path scrub of an already-written brief would do.
	// A brief's "Measured on <date> in <workstation path> at <sha>" line is the measurement,
	// not code, and rewriting it retroactively destroys the only evidence of what was true
	// when the measurement was taken.
	".claude/records/",
	"archive/",
	"CHANGELOG.md",
	"BACKLOG.md",
}

// Exemption is a live file allowed to carry the address, with a reason and a sunset.
type Exemption struct {
	Path   string
	Reason string
	Sunset string // YYYY-MM-DD
}

// DefaultExemptions carries the debt this gate cannot resolve by editing text.
var DefaultExemptions = []Exemption{
	{
		Path:   "infra/local/Caddyfile",
		Sunset: "2026-11-17",
		Reason: "Os três `root *` são caminhos de IMPLANTAÇÃO que resolvem no droplet e servem o " +
			"app-dev hoje. Parametrizar com `{$VAR:default}` mantém o literal no arquivo e não " +
			"fecha nada; parametrizar sem default quebra a borda se a variável não estiver posta " +
			"onde o Caddy sobe. O conserto durável é tirar o dashboard da casa de uma pessoa — " +
			"migração de ambiente, com risco próprio, e não uma edição de higiene. Medido " +
			"2026-08-17 no B-130; registrado como item próprio.",
	},
}

// Config drives the check.
type Config struct {
	RepoRoot   string
	Exemptions []Exemption
}

// Finding is one leak.
type Finding struct {
	File string
	Line int
	Text string
	Kind string // "home" — a metade de IP não foi transportada; ver o doc do pacote
}

// Outcome reports the scan.
type Outcome struct {
	Scanned  int
	Findings []Finding
	Exempt   int
}

// Check scans every versioned live file.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	exempt := map[string]bool{}
	for _, e := range cfg.exemptions() {
		if strings.TrimSpace(e.Reason) == "" || strings.TrimSpace(e.Sunset) == "" {
			return out, fmt.Errorf("%w: isenção de %q sem razão ou sem sunset", ErrLeak, e.Path)
		}
		exempt[e.Path] = true
	}

	files, err := versionedFiles(root)
	if err != nil {
		return out, err
	}
	for _, rel := range files {
		if isAuditTrail(rel) {
			continue
		}
		if exempt[rel] {
			out.Exempt++
			continue
		}
		out.Scanned++
		fs, ferr := scanFile(filepath.Join(root, rel), rel)
		if ferr != nil {
			continue // binário ou ilegível: não é achado deste gate
		}
		out.Findings = append(out.Findings, fs...)
	}
	if out.Scanned == 0 {
		return out, ErrNothingScanned
	}
	if len(out.Findings) > 0 {
		return out, ErrLeak
	}
	return out, nil
}

func (c Config) exemptions() []Exemption {
	if c.Exemptions == nil {
		return DefaultExemptions
	}
	return c.Exemptions
}

// isAuditTrail reports whether a path is a dated record that must not be rewritten.
func isAuditTrail(rel string) bool {
	for _, p := range auditTrailPrefixes {
		if rel == p || strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

// versionedFiles lists what git tracks — what will be published, not the working dir.
func versionedFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = root
	body, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files em %s: %w", root, err)
	}
	var out []string
	for _, l := range strings.Split(string(body), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out, nil
}

func scanFile(abs, rel string) ([]Finding, error) {
	f, err := os.Open(filepath.Clean(abs))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Finding
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		if homeRE.MatchString(line) && !syntheticHomeRE.MatchString(line) && !urlHomeRE.MatchString(line) {
			out = append(out, Finding{File: rel, Line: n, Text: strings.TrimSpace(line), Kind: "home"})
		}
	}
	return out, sc.Err()
}
