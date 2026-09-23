// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package ecosystemvalidators runs the two validators the ecosystem's own change
// protocol requires — and which nothing ran.
//
// # O achado
//
// `rules/cycle-rule-schema.md § Golden Rule Change Protocol` exige, para toda mudança
// numa golden rule LOCKED, que *"`check_xrefs.py` and `test_e2e_smoke.py` both PASS"*.
//
// Medido 2026-08-17 ao cumprir esse protocolo no `B-127`:
// `grep -rn "check_xrefs" taskfiles/ cmd/loopzero-ops/ .claude/hooks/` **não retorna
// nada** — nenhuma task, nenhum gate, nenhum hook os invoca. E ao rodá-los pela
// primeira vez, `check_xrefs.py` saiu **FAIL com 4 achados**, todos introduzidos no
// mesmo dia sem que nada acusasse.
//
// É a mesma forma do `B-124` (três suítes vermelhas que nenhuma task rodava), noutro
// par de validadores: **um requisito de processo sem execução é uma promessa**.
//
// # FAIL e WARN são reportados separados, de propósito
//
// `check_xrefs.py` sai 0 com WARNs. Um gate que somasse os dois faria o aviso apagar
// a falha — e um validador que sempre reporta avisos ensina a ignorá-lo, que é como
// os 4 achados de FAIL passariam despercebidos entre 6 avisos crônicos.
package ecosystemvalidators

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrValidatorFailed means at least one validator exited non-zero.
var ErrValidatorFailed = errors.New("um validador do ecossistema reprovou — eles são requisito do protocolo de mudança de golden rule, e até hoje nenhum gate os rodava")

// ErrValidatorsMissing means the validator scripts are not on disk.
//
// Ausência é FALHA, nunca skip: o gate existe porque ninguém os rodava, e reportar
// verde por não os encontrar reproduziria o defeito com aparência de cobertura.
var ErrValidatorsMissing = errors.New("os validadores do ecossistema não estão no disco — um gate que não acha o que audita não pode reportar verde")

// Validator is one script to run.
type Validator struct {
	Name string
	Rel  string
	// Args sao os argumentos passados ao script, e existem porque um deles tem
	// DUAS perguntas legitimas e o chamador decide qual esta a fazer.
	//
	// `verify_ecosystem.py` por omissao julga os ultimos 40 commits, que e' a
	// pergunta certa para uma auditoria avulsa: o repositorio segue as suas
	// convencoes? Este pacote corre na cadeia
	// `.git/hooks/pre-push` -> `task quality:gates` -> aqui, e a pergunta ai e'
	// outra: este push pode entrar? Ai so' contam as violacoes que quem empurra
	// ainda consegue consertar.
	//
	// Medido 2026-09-16: com `-40`, tres dos quatro achados ja' estavam em
	// `origin/workspace` e os dois mais proximos sairiam da janela em 11 e 13
	// commits — que nao podiam acontecer, porque o gate recusava os commits que
	// moveriam a janela. Cada peca estava certa e juntas eram um impasse.
	Args []string
}

// DefaultValidators are the two the change protocol names.
//
// Paths follow the kit's `scripts/ → mechanisms/<family>/` relocation
// (the same one recorded in `mechanisms/distribution/install.sh` and the
// `check_install_drift.py` docstring). `test_e2e_smoke.py` was renamed to
// `verify_ecosystem.py` in the same pass — the smoke chain is now one section
// inside a broader ecosystem verification. The Rels here follow the kit's
// current on-disk layout; anchoring them elsewhere makes the gate hunt for
// files that were moved deliberately.
var DefaultValidators = []Validator{
	{Name: "check_xrefs", Rel: "mechanisms/gates/check_xrefs.py"},
	{Name: "verify_ecosystem", Rel: "mechanisms/gates/verify_ecosystem.py", Args: []string{"--introduced"}},
}

// Result is one validator's outcome.
type Result struct {
	Name   string
	Passed bool
	Warns  int
	Tail   string
}

// Outcome reports the run.
type Outcome struct {
	// Advisory means nothing was concluded: the interpreter or the kit itself
	// is absent. AdvisoryReason says which.
	Advisory       bool
	AdvisoryReason string
	Results        []Result
}

// Failed lists the validators that exited non-zero.
func (o Outcome) Failed() []Result {
	var out []Result
	for _, r := range o.Results {
		if !r.Passed {
			out = append(out, r)
		}
	}
	return out
}

// Warnings totals the advisory findings across validators.
func (o Outcome) Warnings() int {
	n := 0
	for _, r := range o.Results {
		n += r.Warns
	}
	return n
}

// Config selects the tree.
type Config struct {
	RepoRoot   string
	EcoRel     string // ecosystem dir; empty resolves the layout
	Validators []Validator
	Timeout    time.Duration
}

const defaultTimeout = 2 * time.Minute

// kitInstalled reports whether eco holds the kit's mechanisms tree. The kit is
// an installed, gitignored dependency in consumer repositories (this one
// included), so a clone may legitimately not have it.
func kitInstalled(eco string) bool {
	fi, err := os.Stat(filepath.Join(eco, "mechanisms"))
	return err == nil && fi.IsDir()
}

// resolveEco returns the ecosystem directory, honouring both layouts.
//
// Plugin (todo consumidor) tem o ecossistema em `.claude/`; standalone (o repo do
// próprio kit) o tem na raiz. Fixar um dos dois faria o gate reportar ausência num
// dos layouts — que é o defeito do `B-120`, noutro lugar.
//
// A sonda mudou de `scripts/` para `mechanisms/` porque o kit relocou tudo em
// `mechanisms/<família>/`. `scripts/` deixou de existir; probar por ela é probar
// por um diretório que nenhum layout carrega, e o gate cai para `root` em ambos.
func resolveEco(root, rel string) string {
	if rel != "" {
		return filepath.Join(root, rel)
	}
	if fi, err := os.Stat(filepath.Join(root, ".claude", "mechanisms")); err == nil && fi.IsDir() {
		return filepath.Join(root, ".claude")
	}
	return root
}

// Check runs every validator.
func Check(cfg Config) (Outcome, error) {
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultTimeout
	}
	vals := cfg.Validators
	if vals == nil {
		vals = DefaultValidators
	}
	eco := resolveEco(cfg.RepoRoot, cfg.EcoRel)

	if !kitInstalled(eco) {
		return Outcome{Advisory: true, AdvisoryReason: "the ecosystem kit is not installed here (no mechanisms/ under " +
			eco + "); it is a gitignored dependency, so a clone without it has nothing to run"}, nil
	}
	if _, err := exec.LookPath("python3"); err != nil {
		return Outcome{Advisory: true, AdvisoryReason: "python3 is not on PATH"}, nil
	}

	var out Outcome
	for _, v := range vals {
		script := filepath.Join(eco, v.Rel)
		if _, err := os.Stat(script); err != nil {
			return out, fmt.Errorf("%w: %s", ErrValidatorsMissing, script)
		}
		out.Results = append(out.Results, run(cfg.RepoRoot, script, v.Name, v.Args, cfg.Timeout))
	}
	if len(out.Failed()) > 0 {
		return out, ErrValidatorFailed
	}
	return out, nil
}

func run(root, script, name string, args []string, timeout time.Duration) Result {
	// G204: o caminho não vem de entrada do usuário. Ele é `DefaultValidators`
	// (constantes do pacote) juntado à raiz resolvida, e `Check` já provou que o
	// arquivo existe antes de chegar aqui. O `Validators` configurável existe para
	// os testes montarem uma árvore própria.
	cmd := exec.Command("python3", append([]string{script}, args...)...) //nolint:gosec // caminho e args de constantes do pacote, não de entrada
	cmd.Dir = root

	done := make(chan struct{})
	var output []byte
	var runErr error
	go func() {
		output, runErr = cmd.CombinedOutput()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-done
		return Result{Name: name, Passed: false, Tail: fmt.Sprintf("estourou %s", timeout)}
	}

	body := string(output)
	return Result{
		Name:   name,
		Passed: runErr == nil,
		Warns:  strings.Count(body, "[WARN]"),
		Tail:   lastMeaningfulLine(body),
	}
}

func lastMeaningfulLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}
