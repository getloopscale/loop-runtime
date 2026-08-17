// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testtargetcoverage

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// # Por que a granularidade passou de módulo para PACOTE
//
// `runsTests` respondia *"existe ALGUM `go test` neste módulo?"* — e nunca olhava o
// padrão. Um alvo `cd infra/tests && go test ./unit/...` marcava o módulo inteiro
// como coberto.
//
// Medido em 2026-08-15 ao executar o `B-092`: `infra/tests` **passava** por causa do
// `./unit/...`, enquanto `scripts/` (12 arquivos), `config/` (6) e `performance/` (23)
// não rodavam em alvo nenhum. **41 arquivos de teste invisíveis num módulo que o gate
// reportava coberto.**
//
// O gate não estava errado no que afirmava; estava errado na granularidade em que
// afirmava — e é a granularidade que decide o que ele consegue ver.
//
// O custo da mudança foi medido antes de decidir: **todo módulo tem um `go test ./...`
// exceto `infra/tests`**, e os padrões restritos dos outros são alvos focados
// ADICIONAIS sobre módulos já cobertos por `./...`. Então a mudança morde num módulo
// só.

// testBearingPackages returns every package directory under module that holds a
// `_test.go`, relative to the module root ("." for the module's own directory).
//
// Pacote SEM teste não entra: exigir alvo para ele transformaria o gate num pedido de
// escrever teste para tudo, que é outra conversa. O que este gate afirma é mais
// estreito e verificável — *o que existe está sendo rodado*.
func testBearingPackages(root, module string) ([]string, error) {
	base := filepath.Join(root, module)
	seen := map[string]bool{}
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr
		}
		if d.IsDir() {
			if prunedDirs[d.Name()] {
				return filepath.SkipDir
			}
			// Um submódulo tem go.mod próprio e é auditado como módulo dele.
			// Contá-lo aqui faria dois módulos disputarem o mesmo pacote.
			if path != base {
				if _, statErr := os.Stat(filepath.Join(path, "go.mod")); statErr == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(base, filepath.Dir(path))
		if relErr != nil {
			return nil //nolint:nilerr
		}
		seen[filepath.ToSlash(rel)] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// normalizeTargets makes a multi-line command block readable by the same
// "cd X ... go test" logic that a single line already satisfies.
//
// # O achado que obrigou isto
//
// `taskfiles/quality.yml:388` roda `./chaos/...` e `./livetest/...` — mas em BLOCO:
//
//	cmds:
//	  - |
//	    cd infra/tests
//	    go test -race ... \
//	      ./charts/... ./chaos/... ./capacity/... ./livetest/...
//
// O `cd` está numa linha e o `go test` noutra, e o casamento por linha não via
// nenhum dos dois juntos. Sem esta normalização, as cinco subárvores apareceriam
// como descobertas e eu as teria ISENTADO — declarando por escrito que não são
// rodadas, quando são. **Uma isenção falsa é pior que a lacuna**: ela documenta uma
// mentira e some com o problema de vez.
//
// Duas transformações, ambas conservadoras:
//
//  1. continuação por `\` é junção literal — não há ambiguidade;
//  2. um `cd X` sozinho na linha passa a prefixar as linhas seguintes, até o próximo
//     `cd` ou o começo de outro item de lista (`- `), que é onde o bloco termina.
func normalizeTargets(text string) string {
	// (1) junta continuações de linha.
	text = strings.ReplaceAll(text, "\\\n", " ")

	var out []string
	cur := ""
	for _, raw := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(stripComment(raw))
		// Um novo item de lista encerra o bloco anterior.
		if strings.HasPrefix(trimmed, "- ") {
			cur = ""
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		}
		if strings.HasPrefix(trimmed, "cd ") && !strings.Contains(trimmed, "&&") {
			cur = strings.TrimSpace(strings.TrimPrefix(trimmed, "cd "))
			out = append(out, raw)
			continue
		}
		if cur != "" && strings.Contains(trimmed, "go test") {
			out = append(out, "cd "+cur+" && "+trimmed)
			continue
		}
		out = append(out, raw)
	}
	return strings.Join(out, "\n")
}

// patternsFor extracts the `go test` patterns declared for one module across the
// Taskfile targets.
//
// Reuses the two boundaries `runsTests` already earned by review: the trailing space
// after the module name (so `cd operators/api` does not satisfy `operators`), and
// cutting the scope at the NEXT `cd` (so a later command testing a different module
// does not count).
func patternsFor(targets, module string) []string {
	var pats []string
	for _, raw := range strings.Split(normalizeTargets(targets), "\n") {
		line := stripComment(raw)
		i := strings.Index(line, "cd "+module+" ")
		if i < 0 {
			continue
		}
		scope := line[i+len("cd "+module+" "):]
		if next := strings.Index(scope, "cd "); next >= 0 {
			scope = scope[:next]
		}
		j := strings.Index(scope, "go test")
		if j < 0 {
			continue
		}
		// Todo token `./…` até o fim do comando é um padrão.
		//
		// A primeira versão parava na primeira flag, e estava errada: as flags vêm
		// ANTES dos padrões na forma real — `go test -race -count=1 -coverprofile=X
		// ./charts/... ./chaos/...`. Aquela regra devolvia zero padrões justamente
		// para o alvo que cobre cinco subárvores, e eu quase as isentei por isso.
		//
		// O que encerra o comando é um operador de shell, não uma flag.
		for _, tok := range strings.Fields(scope[j+len("go test"):]) {
			if tok == "|" || tok == "&&" || tok == ";" || strings.HasPrefix(tok, "2>") {
				break
			}
			if strings.HasPrefix(tok, "./") {
				pats = append(pats, tok)
			}
		}
	}
	return pats
}

// patternCovers reports whether a `go test` pattern matches a package path.
//
// `./...`      → tudo
// `./sub/...`  → `sub` e sua subárvore
// `./sub`      → exatamente `sub`
func patternCovers(pattern, pkg string) bool {
	p := strings.TrimSuffix(strings.TrimPrefix(pattern, "./"), "/")
	switch {
	case p == "..." || p == "":
		return true
	case strings.HasSuffix(p, "/..."):
		prefix := strings.TrimSuffix(p, "/...")
		return pkg == prefix || strings.HasPrefix(pkg, prefix+"/")
	default:
		return pkg == p
	}
}

// uncoveredPackages lists the test-bearing packages of a module that no target runs.
func uncoveredPackages(root, module, targets string) ([]string, error) {
	pkgs, err := testBearingPackages(root, module)
	if err != nil {
		return nil, err
	}
	pats := patternsFor(targets, module)
	var out []string
	for _, pkg := range pkgs {
		covered := false
		for _, pat := range pats {
			if patternCovers(pat, pkg) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, pkg)
		}
	}
	return out, nil
}
