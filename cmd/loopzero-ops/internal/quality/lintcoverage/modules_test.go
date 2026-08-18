// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package lintcoverage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

var repoRoot = qtestutil.MustRepoRoot()

func arvore(t *testing.T, paths ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range paths {
		abs := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte("module x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestTodoDiretorioComGoModEModulo(t *testing.T) {
	root := arvore(t, "api/go.mod", "pkg/go.mod", "cmd/loopzero-ops/go.mod", "operators/api/go.mod")
	mods, err := DiscoverModules(root)
	if err != nil {
		t.Fatalf("descoberta: %v", err)
	}
	if len(mods) != 4 {
		t.Fatalf("esperados 4 módulos, veio %d: %v", len(mods), mods)
	}
	// Ordenados, para que a saída e a linha de base sejam determinísticas.
	if mods[0] != "api" || mods[len(mods)-1] != "pkg" {
		t.Fatalf("a ordem precisa ser estável: %v", mods)
	}
}

// A lista à mão foi a causa-raiz: cinco módulos ficaram de fora dela e ninguém percebeu,
// porque o gate seguia verde. Derivar do disco é o conserto — mas a varredura precisa
// ignorar o que não é código nosso, senão ela vira ruído.
func TestVendorENodeModulesSaoIgnorados(t *testing.T) {
	root := arvore(t,
		"api/go.mod",
		"api/vendor/github.com/x/go.mod",
		"web/node_modules/pkg/go.mod",
		"infra/pulumi/node_modules/y/go.mod",
	)
	mods, err := DiscoverModules(root)
	if err != nil {
		t.Fatalf("descoberta: %v", err)
	}
	if len(mods) != 1 || mods[0] != "api" {
		t.Fatalf("dependência de terceiro não é módulo nosso: %v", mods)
	}
}

// Ausência é FALHA, nunca "nenhum módulo, tudo certo".
func TestZeroModulosReprova(t *testing.T) {
	if _, err := DiscoverModules(t.TempDir()); !errors.Is(err, ErrNoModules) {
		t.Fatalf("zero módulos varridos precisa reprovar; err=%v", err)
	}
}

// A população real: a medição que motivou esta fatia contou OITO.
func TestArvoreRealEncontraOsModulos(t *testing.T) {
	mods, err := DiscoverModules(repoRoot)
	if err != nil {
		t.Fatalf("árvore real: %v", err)
	}
	if len(mods) < 8 {
		t.Fatalf("a árvore real tem ao menos 8 módulos; achei %d: %v", len(mods), mods)
	}
	tem := map[string]bool{}
	for _, m := range mods {
		tem[m] = true
	}
	for _, esperado := range []string{"api", "pkg", "operators", "operators/api", "cmd/loopzero-ops", "infra/tests", "infra/scripts"} {
		if !tem[esperado] {
			t.Errorf("módulo %q não foi descoberto — é um dos que `task lint` não olhava", esperado)
		}
	}
}
