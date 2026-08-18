// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package lintcoverage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// falsoRunner substitui a execução real. É a costura que o `B-121` exige: o DoD dele pede
// "teste que prova que um achado em `pkg` não impede `api` de ser linted", e um teste desses
// não pode depender de rodar `golangci-lint` de verdade — seria lento, dependeria da árvore,
// e não conseguiria FABRICAR a falha que precisa provar.
type falsoRunner struct {
	lint  map[string]int
	fmtc  map[string]int
	falha map[string]error
	visto []string
}

func (f *falsoRunner) Lint(_, mod string) (int, error) {
	f.visto = append(f.visto, mod)
	if e, ok := f.falha[mod]; ok {
		return 0, e
	}
	return f.lint[mod], nil
}

func (f *falsoRunner) Gofmt(_, mod string) (int, error) { return f.fmtc[mod], nil }

func repoCom(t *testing.T, baseline string, mods ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, m := range mods {
		p := filepath.Join(root, m, "go.mod")
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("module x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p := filepath.Join(root, BaselineRelPath)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(baseline), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

// A BALA DO B-121, e a razão de o item existir: `task lint` roda os módulos em `cmds`
// separados e o Task aborta no primeiro que sai != 0 — então um achado em `pkg` impedia
// `api` de ser medido, e a contagem citada num item nomeava o que NÃO tinha sido olhado.
func TestFalhaNumModuloNaoImpedeOsOutros(t *testing.T) {
	root := repoCom(t, `{"lint":{"api":0,"pkg":0},"gofmt":{"api":0,"pkg":0}}`, "api", "pkg")
	r := &falsoRunner{
		lint:  map[string]int{"api": 0},
		falha: map[string]error{"pkg": errors.New("golangci-lint explodiu em pkg")},
	}
	out, err := Check(Config{RepoRoot: root, Runner: r})

	if len(r.visto) != 2 {
		t.Fatalf("os DOIS módulos precisam ser executados; visto=%v", r.visto)
	}
	if err == nil {
		t.Fatal("a falha de um módulo precisa aparecer no veredito agregado")
	}
	if len(out.Failed) != 1 || out.Failed[0].Module != "pkg" {
		t.Fatalf("a falha precisa NOMEAR o módulo: %+v", out.Failed)
	}
}

// A segunda bala do B-121: a saída diz quantos POR MÓDULO, para que uma contagem citada num
// item nomeie o que foi medido.
func TestSaidaDizAContagemPorModulo(t *testing.T) {
	root := repoCom(t, `{"lint":{"api":0,"cmd/loopzero-ops":221},"gofmt":{"api":70,"cmd/loopzero-ops":0}}`, "api", "cmd/loopzero-ops")
	r := &falsoRunner{
		lint: map[string]int{"api": 0, "cmd/loopzero-ops": 221},
		fmtc: map[string]int{"api": 70, "cmd/loopzero-ops": 0},
	}
	out, err := Check(Config{RepoRoot: root, Runner: r})
	if err != nil {
		t.Fatalf("igual ao congelado passa: %v", err)
	}
	if out.Lint["cmd/loopzero-ops"] != 221 || out.Lint["api"] != 0 {
		t.Fatalf("contagem por módulo: %+v", out.Lint)
	}
	if !strings.Contains(out.Reason, "cmd/loopzero-ops") || !strings.Contains(out.Reason, "221") {
		t.Fatalf("a razão precisa carregar os números por módulo: %q", out.Reason)
	}
}

func TestRegressaoReprova(t *testing.T) {
	root := repoCom(t, `{"lint":{"api":0},"gofmt":{"api":0}}`, "api")
	r := &falsoRunner{lint: map[string]int{"api": 1}}
	out, err := Check(Config{RepoRoot: root, Runner: r})
	if !errors.Is(err, ErrRegression) {
		t.Fatalf("regressão reprova; err=%v", err)
	}
	if len(out.Regressions) != 1 || out.Regressions[0].Module != "api" {
		t.Fatalf("a regressão precisa nomear o módulo: %+v", out.Regressions)
	}
}

// B-148: o `gofmt` é dimensão PRÓPRIA, com catraca própria — `pkg` tem 30 violações de
// formato e 0 de golangci-lint, então uma dimensão não cobre a outra.
func TestGofmtTemCatracaPropria(t *testing.T) {
	root := repoCom(t, `{"lint":{"pkg":0},"gofmt":{"pkg":30}}`, "pkg")
	r := &falsoRunner{lint: map[string]int{"pkg": 0}, fmtc: map[string]int{"pkg": 31}}
	out, err := Check(Config{RepoRoot: root, Runner: r})
	if !errors.Is(err, ErrRegression) {
		t.Fatalf("regressão de formato reprova mesmo com lint em 0; err=%v", err)
	}
	if len(out.Regressions) != 1 || out.Regressions[0].Dimension != "gofmt" {
		t.Fatalf("a dimensão precisa aparecer: %+v", out.Regressions)
	}
}

func TestModuloNaoDeclaradoReprova(t *testing.T) {
	root := repoCom(t, `{"lint":{"api":0},"gofmt":{"api":0}}`, "api", "modulo/novo")
	r := &falsoRunner{lint: map[string]int{"api": 0, "modulo/novo": 5}}
	if _, err := Check(Config{RepoRoot: root, Runner: r}); !errors.Is(err, ErrUndeclaredModule) {
		t.Fatalf("módulo novo entra declarado, nunca em silêncio; err=%v", err)
	}
}

func TestBaseAusenteReprovaNoGate(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "api", "go.mod")
	_ = os.MkdirAll(filepath.Dir(p), 0o750)
	_ = os.WriteFile(p, []byte("module x\n"), 0o600)
	if _, err := Check(Config{RepoRoot: root, Runner: &falsoRunner{}}); !errors.Is(err, ErrBaselineMissing) {
		t.Fatalf("sem congelado não há verde possível; err=%v", err)
	}
}

// `--write` precisa MEDIR sem exigir a linha de base, senão o primeiro congelamento é
// impossível: `Check` reprova em base ausente antes de medir qualquer coisa. Medir e julgar
// são responsabilidades diferentes, e a primeira versão desta fatia as confundiu.
func TestMedirNaoExigeLinhaDeBase(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "api", "go.mod")
	_ = os.MkdirAll(filepath.Dir(p), 0o750)
	_ = os.WriteFile(p, []byte("module x\n"), 0o600)

	r := &falsoRunner{lint: map[string]int{"api": 7}, fmtc: map[string]int{"api": 3}}
	m, err := Measure(Config{RepoRoot: root, Runner: r})
	if err != nil {
		t.Fatalf("medir sem base: %v", err)
	}
	if m.Lint["api"] != 7 || m.Gofmt["api"] != 3 {
		t.Fatalf("medição: %+v", m)
	}
}
