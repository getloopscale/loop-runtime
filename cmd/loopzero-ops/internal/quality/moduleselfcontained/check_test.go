// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package moduleselfcontained

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

var repoRoot = qtestutil.MustRepoRoot()

type falsoBuilder struct {
	falha map[string]error
	visto []string
}

func (f *falsoBuilder) BuildWithoutWorkspace(_, mod string) error {
	f.visto = append(f.visto, mod)
	return f.falha[mod]
}

func arvore(t *testing.T, mods ...string) string {
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
	return root
}

// O DEFEITO MEDIDO em 2026-08-18: `operators/api/go.mod` subiu k8s para v0.36.0 num conserto
// de CVE em 13/ago, e `operators/go.mod` — que depende dele — ficou em v0.35.3. O `go.work`
// resolve entre módulos e mascara; o Dockerfile usa `GOWORK=off` e não. O CI parou de
// publicar QUALQUER imagem por cinco dias, e nada reprovou.
func TestModuloQueSoCompilaComWorkspaceReprova(t *testing.T) {
	root := arvore(t, "operators", "api")
	b := &falsoBuilder{falha: map[string]error{
		"operators": errors.New("go: updates to go.mod needed; to update it:\n\tgo mod tidy"),
	}}
	out, err := Check(Config{RepoRoot: root, Builder: b})
	if !errors.Is(err, ErrNotSelfContained) {
		t.Fatalf("módulo que só compila com go.work reprova; err=%v", err)
	}
	if len(out.Failed) != 1 || out.Failed[0].Module != "operators" {
		t.Fatalf("o achado precisa NOMEAR o módulo: %+v", out.Failed)
	}
}

// Todos são construídos mesmo quando um falha — a mesma bala do B-121: abortar no primeiro
// esconde os seguintes.
func TestFalhaNumModuloNaoImpedeOsOutros(t *testing.T) {
	root := arvore(t, "api", "operators", "pkg")
	b := &falsoBuilder{falha: map[string]error{"api": errors.New("boom")}}
	_, _ = Check(Config{RepoRoot: root, Builder: b})
	if len(b.visto) != 3 {
		t.Fatalf("os três precisam ser construídos; visto=%v", b.visto)
	}
}

func TestTodosAutocontidosPassa(t *testing.T) {
	root := arvore(t, "api", "pkg")
	out, err := Check(Config{RepoRoot: root, Builder: &falsoBuilder{}})
	if err != nil {
		t.Fatalf("todos autocontidos passa: %v", err)
	}
	if out.Checked != 2 {
		t.Fatalf("checked=%d", out.Checked)
	}
}

func TestZeroModulosReprova(t *testing.T) {
	if _, err := Check(Config{RepoRoot: t.TempDir(), Builder: &falsoBuilder{}}); err == nil {
		t.Fatal("zero módulos varridos precisa reprovar")
	}
}

// A população real — lenta de propósito: é ela que prova o invariante na árvore que ship.
func TestArvoreReal(t *testing.T) {
	out, err := Check(Config{RepoRoot: repoRoot})
	if err != nil {
		for _, f := range out.Failed {
			t.Errorf("%s não compila sem go.work:\n%s", f.Module, f.Err)
		}
		t.Fatalf("árvore real: %v", err)
	}
	if out.Checked == 0 {
		t.Fatal("zero módulos verificados — não achar nada não é o mesmo que não achar nada errado")
	}
}
