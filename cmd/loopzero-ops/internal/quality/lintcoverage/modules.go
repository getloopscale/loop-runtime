// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package lintcoverage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrNoModules means the sweep found no Go module.
//
// Ausência é FALHA, nunca "nenhum módulo, tudo certo": uma varredura que não alcançou nada
// reportando verde é a forma que este repositório já pagou em `B-040`, `B-124` e `B-143`.
var ErrNoModules = errors.New("nenhum módulo Go encontrado — não achar nada não é o mesmo que não achar nada errado")

// skipDirs são subárvores que contêm `go.mod` de terceiros.
//
// Lista fechada e curta de propósito. O que ela NÃO faz é listar os módulos nossos — foi
// exatamente uma lista à mão de módulos que deixou cinco de fora do `task lint` e ninguém
// percebeu, porque o gate seguia verde sobre o que não olhava.
var skipDirs = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	".git":         true,
	"testdata":     true,
}

// DiscoverModules lists every Go module in the tree, relative to root.
//
// Derivado do DISCO, nunca declarado: um módulo novo entra sozinho, que é a propriedade que
// faltava. A ordenação é estável para que a saída e a linha de base sejam determinísticas —
// uma linha de base cuja ordem varia produz diff que ninguém consegue revisar.
func DiscoverModules(root string) ([]string, error) {
	var out []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // diretório ilegível não é módulo; a ausência total reprova abaixo
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Name() != "go.mod" {
			return nil
		}
		rel, rerr := filepath.Rel(root, filepath.Dir(p))
		if rerr != nil {
			return nil //nolint:nilerr // caminho fora da raiz não é módulo nosso
		}
		if rel == "." {
			return nil // não há go.mod na raiz deste repositório (ver CLAUDE.md § Multi-module)
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("varrendo %s: %w", root, err)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, ErrNoModules
	}
	return out, nil
}

// ModuleDir resolves a module's absolute directory.
func ModuleDir(root, mod string) string {
	return filepath.Join(root, filepath.FromSlash(mod))
}

// IsToolModule reports whether the module lives under tools/.
func IsToolModule(mod string) bool { return strings.HasPrefix(mod, "tools/") }
