// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package moduleselfcontained refuses a Go module that only builds inside the workspace.
//
// # O defeito, medido
//
// Em 2026-08-18, ao tentar levar um conserto ao droplet, descobri que o CI **não publicava
// nenhuma imagem desde 2026-08-09**. Causa:
//
//	operators/api/go.mod   k8s.io/apimachinery v0.36.0   <- subiu em 13/ago, conserto de CVE
//	operators/go.mod       k8s.io/api          v0.35.3   <- ficou para tras
//
// `operators` **depende** de `operators/api`. Sob MVS a versão maior ganha, então o módulo
// precisa declarar ≥0.36.0. Ele não declarava, e o build morria com
// `go: updates to go.mod needed`.
//
// **E nada reprovou por cinco dias**, porque `go.work` une os módulos e resolve entre eles —
// `go build ./...` local passa. O Dockerfile usa `GOWORK=off` (como qualquer build isolado), e
// aí a inconsistência aparece. O `CLAUDE.md` já avisa que *"`go build ./...` from the root does
// NOT cover the repo"*; o que faltava era alguém verificar.
//
// # O invariante
//
// Todo módulo compila **sem** o workspace. É a condição que o CI exige e que o
// desenvolvimento local nunca exercita — exatamente a forma de defeito que envelhece em
// silêncio.
//
// # O custo, dito em voz alta
//
// Este gate compila cada módulo de verdade, com o cache de módulos frio na primeira vez. É
// caro, e é caro pela mesma razão que é útil: é o que o CI faz. Ele roda no `quality:gates`,
// não em cada teste.
package moduleselfcontained

import (
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/lintcoverage"
)

// ErrNotSelfContained means a module does not build without the workspace.
var ErrNotSelfContained = errors.New("módulo não compila sem o workspace — o CI não conseguirá construí-lo")

// Builder compiles one module in isolation. Costura injetável (DIP): o teste precisa
// FABRICAR a falha, e compilar de verdade levaria minutos e dependeria da árvore.
type Builder interface {
	BuildWithoutWorkspace(root, module string) error
}

// Config drives the check.
type Config struct {
	RepoRoot string
	Builder  Builder
}

// Failure is one module that does not stand alone.
type Failure struct {
	Module string
	Err    string
}

// Outcome reports what was established.
type Outcome struct {
	Checked int
	Failed  []Failure
	Reason  string
}

// Check builds every module with the workspace disabled.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	builder := cfg.Builder
	if builder == nil {
		builder = execBuilder{}
	}

	// Reaproveita a descoberta do `lintcoverage` em vez de reimplementá-la: a lição de lá é
	// que uma lista à mão envelhece para o lado invisível, e duas listas divergem.
	mods, err := lintcoverage.DiscoverModules(root)
	if err != nil {
		return out, err
	}

	// TODOS são construídos, mesmo quando um falha — abortar no primeiro esconde os
	// seguintes, que é o defeito que o B-121 mediu no `task lint`.
	for _, m := range mods {
		out.Checked++
		if berr := builder.BuildWithoutWorkspace(root, m); berr != nil {
			out.Failed = append(out.Failed, Failure{Module: m, Err: berr.Error()})
		}
	}
	sort.Slice(out.Failed, func(i, j int) bool { return out.Failed[i].Module < out.Failed[j].Module })

	if len(out.Failed) > 0 {
		f := out.Failed[0]
		return out, fmt.Errorf("%w: %s — %s", ErrNotSelfContained, f.Module, firstLine(f.Err))
	}
	out.Reason = fmt.Sprintf("%d módulo(s) compilam sem o workspace (GOWORK=off), que é o que o CI faz", out.Checked)
	return out, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// execBuilder runs the real compiler with the workspace disabled.
type execBuilder struct{}

func (execBuilder) BuildWithoutWorkspace(root, mod string) error {
	// #nosec G204 -- argumentos literais; `dir` deriva da varredura de go.mod sob a raiz.
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = lintcoverage.ModuleDir(root, mod)
	cmd.Env = append(cmd.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	// `go build` sai 0 mesmo quando imprime "updates to go.mod needed" — medido em
	// 2026-08-18, e foi por isso que a inconsistencia atravessou cinco dias. O texto e o
	// sinal, nao o codigo de saida.
	if strings.Contains(string(out), "updates to go.mod needed") {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}
