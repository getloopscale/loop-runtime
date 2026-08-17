// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package testinventory derives the test pyramid FROM THE TREE, instead of reading
// numbers a file declares about itself.
//
// # O defeito que isto remove
//
// `infra/quality/test-inventory.json` era config viva mantida à mão, e o
// `testpyramid` lia três percentuais **escritos no arquivo**. Medido em 2026-08-17:
//
//	arquivos de teste declarados      710
//	desses, inexistentes no disco     335   (47%)
//	arquivos `_test.go` no disco      988
//	no disco e AUSENTES do inventário 649
//
// O inventário descrevia ~375 arquivos reais de 988: **62% cego**. E a pirâmide que
// ele declarava (99,4% unit) era calculada sobre essa amostra, que exclui a suíte
// `infra/tests/e2e` inteira.
//
// O gate comparava um limiar contra um número que o próprio arquivo afirmava. **Um
// arquivo podia declarar a pirâmide que quisesse** — e a que declarava já não era a
// do repositório em nenhuma das quatro linhas.
//
// Consertar os 649 à mão consertaria a aparência e não o mecanismo: a lista voltaria
// a divergir na primeira fatia que movesse um teste. Derivada da árvore, ela não pode
// divergir — não há o que atualizar.
//
// # A classificação é heurística, e isto está escrito de propósito
//
// Se um teste é unitário ou de integração é, no limite, juízo editorial. O que este
// pacote faz é aplicar UMA regra declarada, na mesma ordem, sempre:
//
//  1. **build tag** — o autor declarou. `live` é e2e, `helm_render` é integração.
//  2. **caminho** — as subárvores cujo propósito é o próprio nome.
//  3. **unit** — o resto.
//
// Build tag antes de caminho porque a tag é uma declaração explícita de quem escreveu
// o teste, e o caminho é inferência de quem escreveu este classificador.
package testinventory

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Category is a pyramid tier.
type Category string

const (
	Unit        Category = "unit"
	Integration Category = "integration"
	E2E         Category = "e2e"
)

// Entry is one classified test file.
type Entry struct {
	Path     string
	Category Category
	// Reason names WHICH rule classified it. Sem isto o inventário derivado teria
	// o mesmo defeito do declarado: um número sem como conferir de onde veio.
	Reason string
}

// Inventory is the derived state of the tree.
type Inventory struct {
	Entries []Entry
	Unit    int
	Integr  int
	E2E     int
}

// Total counts every classified file.
func (i Inventory) Total() int { return len(i.Entries) }

// Ratios returns the three percentages, derived.
func (i Inventory) Ratios() (unitPct, integrationPct, e2ePct float64) {
	n := float64(i.Total())
	if n == 0 {
		return 0, 0, 0
	}
	return float64(i.Unit) / n * 100, float64(i.Integr) / n * 100, float64(i.E2E) / n * 100
}

// e2ePrefixes are subtrees whose whole purpose is end-to-end exercise.
var e2ePrefixes = []string{
	"infra/tests/e2e/",
	"infra/tests/livetest/",
	"infra/tests/chaos/",
	"infra/tests/performance/",
}

// integrationPrefixes are subtrees that cross a real boundary — a rendered chart, a
// running cluster, a database.
var integrationPrefixes = []string{
	"infra/tests/charts/",
	"infra/tests/integration/",
	"api/tests/",
}

// e2eTags / integrationTags are build tags whose meaning is declared by the author.
var (
	e2eTags         = []string{"live"}
	integrationTags = []string{"helm_render", "integration"}
)

// Classify decides the tier for one file.
//
// `header` is the top of the file — build constraints must appear before the package
// clause, so reading past it would only add cost.
func Classify(path, header string) (Category, string) {
	for _, tag := range e2eTags {
		if hasBuildTag(header, tag) {
			return E2E, "build tag " + tag
		}
	}
	for _, tag := range integrationTags {
		if hasBuildTag(header, tag) {
			return Integration, "build tag " + tag
		}
	}
	slash := filepath.ToSlash(path)
	for _, p := range e2ePrefixes {
		if strings.HasPrefix(slash, p) {
			return E2E, "caminho " + p
		}
	}
	for _, p := range integrationPrefixes {
		if strings.HasPrefix(slash, p) {
			return Integration, "caminho " + p
		}
	}
	return Unit, "padrão"
}

func hasBuildTag(header, tag string) bool {
	for _, line := range strings.Split(header, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "//go:build") {
			continue
		}
		// Casa o token inteiro: `live` não pode casar dentro de `livetest`.
		for _, tok := range strings.FieldsFunc(line[len("//go:build"):], func(r rune) bool {
			return r == ' ' || r == '(' || r == ')' || r == '&' || r == '|' || r == '!'
		}) {
			if tok == tag {
				return true
			}
		}
	}
	return false
}

// skipDirs are trees that hold foreign or generated code. Counting them would make
// the pyramid describe someone else's tests.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
	".claude": true, "archive": true, "testdata": true,
}

// Derive walks the tree and classifies every Go test file.
func Derive(root string) (Inventory, error) {
	var inv Inventory
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // um diretório ilegível não invalida a árvore
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil //nolint:nilerr
		}
		cat, reason := Classify(filepath.ToSlash(rel), readHeader(path))
		inv.Entries = append(inv.Entries, Entry{Path: filepath.ToSlash(rel), Category: cat, Reason: reason})
		switch cat {
		case E2E:
			inv.E2E++
		case Integration:
			inv.Integr++
		case Unit:
			inv.Unit++
		}
		return nil
	})
	if err != nil {
		return inv, fmt.Errorf("walk %s: %w", root, err)
	}
	return inv, nil
}

// readHeader returns the first lines of a file, where build constraints live.
func readHeader(path string) string {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return ""
	}
	defer f.Close()
	var b strings.Builder
	sc := bufio.NewScanner(f)
	for n := 0; n < 20 && sc.Scan(); n++ {
		line := sc.Text()
		b.WriteString(line)
		b.WriteString("\n")
		// A cláusula `package` encerra a região das build constraints.
		if strings.HasPrefix(strings.TrimSpace(line), "package ") {
			break
		}
	}
	return b.String()
}
