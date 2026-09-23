// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package lintcoverage measures every Go module, and refuses regression per module.
//
// # O defeito, medido
//
// `task lint` (`taskfiles/test.yml:512-531`) rodava TRÊS módulos de OITO, em três entradas
// `cmds` separadas — e o Task aborta na primeira que sai != 0, então um achado em `pkg`
// impedia `api` de ser medido. Foi por isso que o `B-009` reportou *"11 achados, todos em
// operators"*: `api` nunca tinha sido olhado.
//
// Medido em 2026-08-18, módulo a módulo:
//
//	pkg  operators  api  operators/api  tools/*(2)        0 cada   <- os cobertos
//	infra/scripts                                          4
//	infra/tests                                           93
//	cmd/loopzero-ops                                         221       <- o binario dos 130 gates
//
// **318 achados em módulos que ninguém lint**, e o maior é a ferramenta que executa todos os
// outros gates deste repositório. O `B-121` descreve o sintoma menor — abortar no primeiro — e
// perde o maior: mesmo sem abortar, a task olharia 3 de 8.
//
// # Por que catraca, e não "conserte os 318"
//
// `task lint` é linha do `CLAUDE.md § Pre-completion checklist`. Cobrir tudo sem catraca a leva
// de verde para 318 achados, e uma linha de checklist que ninguém consegue marcar deixa de ser
// checklist — a queixa literal do `B-083`. A trava seria desligada, levando junto a cobertura
// que acabou de ganhar.
//
// A catraca torna os 318 visíveis e impede o 319: um módulo em 0 que ganhe um achado reprova,
// mesmo com 221 congelados ao lado.
//
// # O `gofmt` é dimensão própria
//
// 155 arquivos fora do formato, e `pkg` tem 30 deles com **0** achados de `golangci-lint` —
// nenhum linter de formatação está habilitado no `.golangci.yml`. Uma dimensão não cobre a
// outra, e elas se corrigem de formas diferentes: formatação é mecânica, `gosec` não é.
//
// # A execução entra por costura injetável
//
// O DoD do `B-121` exige *"teste que prova que um achado em `pkg` não impede `api` de ser
// linted"* — e um teste desses não pode depender de rodar `golangci-lint` de verdade: seria
// lento, dependeria da árvore, e não conseguiria FABRICAR a falha que precisa provar.
//
// Decisão registrada em
// `.claude/knowledge-base/decisions/MEET-ASYNC-AMENDMENT-2026-08-18-lint-covers-every-module.md`
// (T2, pendente de ratificação).
package lintcoverage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ErrRegression means a module got worse than its frozen count.
var ErrRegression = errors.New("regressão contra a linha de base")

// ErrUndeclaredModule means a module has no frozen entry.
var ErrUndeclaredModule = errors.New("módulo sem entrada na linha de base")

// ErrRunnerFailed means a module could not be measured.
//
// NÃO é "zero achados": um módulo que não pôde ser medido reportado como limpo é o
// verde-sobre-nada que este gate existe para impedir.
var ErrRunnerFailed = errors.New("módulo não pôde ser medido")

// Runner measures one module. É a costura (DIP) que torna a política testável sem processo
// externo — ver o doc do pacote.
type Runner interface {
	Lint(root, module string) (int, error)
	Gofmt(root, module string) (int, error)
}

// Config drives the check.
type Config struct {
	RepoRoot     string
	BaselinePath string
	// Runner defaults to the real golangci-lint / gofmt invocation.
	Runner Runner
}

// Failure is one module that could not be measured.
type Failure struct {
	Module string
	Err    string
}

// Outcome reports what was established.
type Outcome struct {
	Modules      []string
	Lint         map[string]int
	Gofmt        map[string]int
	Regressions  []Regression
	Improvements []Improvement
	Undeclared   []string
	Failed       []Failure
	Reason       string
}

// Measure runs every module and returns the counts, WITHOUT judging them.
//
// Separado de Check de propósito (SRP), e a razão é concreta: `--write` precisa medir para
// poder congelar pela primeira vez, e `Check` reprova em linha de base ausente antes de medir
// qualquer coisa. A primeira versão desta fatia confundiu as duas responsabilidades e tornou o
// primeiro congelamento impossível.
func Measure(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	runner := cfg.Runner
	if runner == nil {
		runner = execRunner{}
	}

	mods, err := DiscoverModules(root)
	if err != nil {
		return out, err
	}
	out.Modules = mods

	// TODOS os módulos são medidos, mesmo quando um falha. É a bala do B-121: abortar no
	// primeiro é o que mantinha 5 módulos e 318 achados invisíveis.
	out.Lint = map[string]int{}
	out.Gofmt = map[string]int{}
	for _, m := range mods {
		n, lerr := runner.Lint(root, m)
		if lerr != nil {
			out.Failed = append(out.Failed, Failure{Module: m, Err: lerr.Error()})
		} else {
			out.Lint[m] = n
		}
		g, gerr := runner.Gofmt(root, m)
		if gerr != nil {
			out.Failed = append(out.Failed, Failure{Module: m, Err: gerr.Error()})
		} else {
			out.Gofmt[m] = g
		}
	}
	out.Reason = summarize(mods, out.Lint, out.Gofmt)
	return out, nil
}

// Check measures every module and applies the ratchet.
func Check(cfg Config) (Outcome, error) {
	out, err := Measure(cfg)
	if err != nil {
		return out, err
	}
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	basePath := cfg.BaselinePath
	if basePath == "" {
		basePath = filepath.Join(root, BaselineRelPath)
	}
	base, err := LoadBaseline(basePath)
	if err != nil {
		return out, err
	}

	vl := base.Compare(out.Lint, base.Lint, "lint")
	vg := base.Compare(out.Gofmt, base.Gofmt, "gofmt")
	out.Regressions = append(vl.Regressions, vg.Regressions...)
	out.Improvements = append(vl.Improvements, vg.Improvements...)
	out.Undeclared = dedup(append(vl.Undeclared, vg.Undeclared...))

	if len(out.Failed) > 0 {
		return out, fmt.Errorf("%w: %s", ErrRunnerFailed, out.Failed[0].Module+": "+out.Failed[0].Err)
	}
	if len(out.Regressions) > 0 {
		r := out.Regressions[0]
		return out, fmt.Errorf("%w: %s [%s] %d → %d — a linha de base recusa regressão; conserte, ou recongele com `--write` explicando por quê",
			ErrRegression, r.Module, r.Dimension, r.Was, r.Now)
	}
	if len(out.Undeclared) > 0 {
		return out, fmt.Errorf("%w (%d): %s — um módulo novo entra DECLARADO, nunca em silêncio; rode `--write`",
			ErrUndeclaredModule, len(out.Undeclared), strings.Join(out.Undeclared, ", "))
	}
	return out, nil
}

// summarize renders the per-module counts — a segunda bala do B-121.
func summarize(mods []string, lint, gofmtCounts map[string]int) string {
	parts := make([]string, 0, len(mods))
	tl, tg := 0, 0
	for _, m := range mods {
		parts = append(parts, fmt.Sprintf("%s=%d/%d", m, lint[m], gofmtCounts[m]))
		tl += lint[m]
		tg += gofmtCounts[m]
	}
	return fmt.Sprintf("%d módulo(s) medido(s) [lint/gofmt]: %s — total %d achados, %d arquivos fora de formato",
		len(mods), strings.Join(parts, " "), tl, tg)
}

func dedup(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// findingRE matches one golangci-lint finding line (`path/file.go:12:3: message`).
var findingRE = regexp.MustCompile(`(?m)^[a-zA-Z0-9_./-]+\.go:[0-9]+:[0-9]+:`)

// execRunner invokes the real tools.
type execRunner struct{}

// lintCacheDir is the golangci-lint cache this module's run may use: one
// directory per module directory, derived rather than shared.
//
// The shared default cache replays the analysis of trees that no longer exist. On
// 2026-09-05 `golangci-lint run ./...` in api/ reported 35 findings, all of them
// under /tmp/b059-lint/api/ and /tmp/clone-B-059/api/ -- neither on disk, neither a
// registered worktree, both debris from an earlier lane. The same run with a
// private cache reported 0. The gate faithfully turned that into `api [lint] 0 ->
// 35` and failed the chain on a module whose own lint is clean.
//
// A false FAIL costs what a false PASS costs. It teaches a reader that the chain's
// red means nothing in particular, and the next real regression is read as more of
// the same.
//
// Keyed by path hash rather than isolated per run, so caching still works: the same
// tree reuses its own cache, and no tree can answer for another.
func lintCacheDir(dir string) string {
	sum := sha256.Sum256([]byte(dir))
	return filepath.Join(os.TempDir(), "loopzero-golangci-"+hex.EncodeToString(sum[:8]))
}

// lintCommand builds the invocation, separated from running it so the environment
// it carries can be asserted without golangci-lint on the box.
func lintCommand(dir string) *exec.Cmd {
	// #nosec G204 -- `dir` deriva da varredura de go.mod sob a raiz; nenhum argumento vem de
	// entrada de usuário.
	cmd := exec.Command("golangci-lint", "run", "--timeout", "5m", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOLANGCI_LINT_CACHE="+lintCacheDir(dir))
	return cmd
}

func (execRunner) Lint(root, mod string) (int, error) {
	dir := ModuleDir(root, mod)
	cmd := lintCommand(dir)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			// A missing or unrunnable binary is NOT "zero findings".
			return 0, fmt.Errorf("golangci-lint in %s: %w", mod, err)
		}
		code = exitErr.ExitCode()
	}
	n, lerr := lintFindings(code, out)
	if lerr != nil {
		return 0, fmt.Errorf("golangci-lint in %s: %w", mod, lerr)
	}
	return n, nil
}

// lintIssuesExitCode is golangci-lint's exit code for "issues were found".
// Every other non-zero code is a run that did not complete.
const lintIssuesExitCode = 1

// lintFindings turns a golangci-lint exit code and output into a count. A
// crashed run (config error, type-check failure, timeout) prints no finding
// lines; counting it as zero would report an unmeasured module as clean.
func lintFindings(exitCode int, out []byte) (int, error) {
	n := len(findingRE.FindAll(out, -1))
	switch {
	case exitCode == 0:
		return n, nil
	case exitCode == lintIssuesExitCode && n > 0:
		return n, nil
	default:
		return 0, fmt.Errorf("exit %d with %d parseable finding(s) — the run did not complete: %s",
			exitCode, n, lastLine(out))
	}
}

func lastLine(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return lines[len(lines)-1]
}

func (execRunner) Gofmt(root, mod string) (int, error) {
	dir := ModuleDir(root, mod)
	// #nosec G204 -- mesma justificativa do Lint acima.
	cmd := exec.Command("gofmt", "-l", ".")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("gofmt em %s: %w", mod, err)
	}
	n := 0
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.Contains(l, "/vendor/") {
			n++
		}
	}
	return n, nil
}
