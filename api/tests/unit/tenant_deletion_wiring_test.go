// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"go/ast"
	"go/parser"
	"go/token"
	gotypes "go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestTenantDeletionStore_EhAtribuidoNaMontagemDeProducao is the assertion whose
// absence let account deletion be broken for months (B-117).
//
// # Why the existing tests did not catch it
//
// `router_wiring_test.go` already passes a `stubTenantDeletionStore` and asserts
// the three routes register. That test is correct and it passed the whole time —
// it proves the `if cfg.TenantDeletionStore != nil` branch works.
//
// What nothing proved is that the condition is ever TRUE in production. The
// field was declared and never assigned: the sqlc pilot that created
// `TenantDeletionStoreSqlc` deleted the legacy store in the same commit and
// never wired the replacement, so the type was born unreachable and a
// code-quality pass removed it three weeks later as dead.
//
// Meanwhile the dashboard's account-deletion screen called
// `DELETE /api/v1/tenants/{id}` for real, and two rounds of client-side bug
// fixes were spent on a request that could not arrive.
//
// A test that stubs the dependency can only ever prove the guard. Proving the
// guard is SATISFIED requires looking at the assembly, which is what this does.
func TestTenantDeletionStore_EhAtribuidoNaMontagemDeProducao(t *testing.T) {
	const cmdDir = "../../cmd"

	found, where := assignmentSites(t, cmdDir, "TenantDeletionStore")
	if !found {
		t.Fatalf("nenhuma atribuição a `TenantDeletionStore` em %s — as três rotas de "+
			"exclusão de tenant existem no roteador, o handler existe, e o painel chama "+
			"`DELETE /api/v1/tenants/{id}` de verdade, mas o campo fica nil e a rota "+
			"nunca é registrada (B-117)", cmdDir)
	}
	t.Logf("atribuído em: %s", strings.Join(where, ", "))
}

// TestTodoCampoGuardadoNoRoteadorEhAtribuidoAlgumaVez generalises the assertion
// above to the whole population, which is the step B-109 exists to demand:
// proving a guard SEES a shape is not the same as measuring how many instances
// of that shape already exist.
//
// Measured 2026-08-16: 53 fields are guarded by `cfg.X != nil` in the router and
// exactly ONE — `TenantDeletionStore` — was never assigned. The instance was the
// entire class. A measured zero and an assumed zero are different facts, and
// this test is what keeps the first one true.
//
// It is deliberately NOT a loopzero-ops gate. `silent-surface` answers a different
// question — does the guard REPORT its decision — and a surface can report
// `false` forever while nothing is broken about the reporting. This one answers
// whether the condition can ever be true at all. Same file, same suite, no new
// registration to wire or oracle to re-freeze.
func TestTodoCampoGuardadoNoRoteadorEhAtribuidoAlgumaVez(t *testing.T) {
	const (
		routerPath = "../../internal/server/router.go"
		cmdDir     = "../../cmd"
	)

	src, err := os.ReadFile(routerPath)
	if err != nil {
		t.Fatalf("read router: %v", err)
	}
	campos := guardedConfigFields(string(src))
	if len(campos) == 0 {
		t.Fatal("nenhum campo guardado encontrado no roteador — ou ele mudou de forma, " +
			"ou este teste parou de o reconhecer. Um teste que não mede nada não verifica nada")
	}

	var orfaos []string
	for _, c := range campos {
		if ok, _ := assignmentSites(t, cmdDir, c); !ok {
			orfaos = append(orfaos, c)
		}
	}
	if len(orfaos) > 0 {
		t.Errorf("%d de %d campos guardados no roteador NUNCA são atribuídos em %s: %v.\n"+
			"Cada um é uma rota que existe no código e não existe em deployment nenhum — "+
			"um teste que injeta um stub prova o `if`, não a condição (B-117)",
			len(orfaos), len(campos), cmdDir, orfaos)
	}
}

// guardedConfigFields extracts every `cfg.X != nil` field name from the router.
func guardedConfigFields(src string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range regexp.MustCompile(`cfg\.(\w+) != nil`).FindAllStringSubmatch(src, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// assignmentSites walks a directory of production Go files and reports whether
// any of them assigns the named struct field, and where.
//
// It reads the SOURCE rather than running the assembly because the assembly
// needs a database. The trade is explicit: this catches "nobody wires it",
// which is the failure that happened, and does NOT catch "it is wired to
// something broken", which the store's own tests own.
func assignmentSites(t *testing.T, dir, field string) (bool, []string) {
	t.Helper()

	var sites []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		parsed, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.AssignStmt:
				// `sc.TenantDeletionStore = …`
				for _, lhs := range node.Lhs {
					if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == field {
						sites = append(sites, positionOf(fset, path, node.Pos()))
					}
				}
			case *ast.KeyValueExpr:
				// `TenantDeletionStore: …` inside a composite literal.
				if id, ok := node.Key.(*ast.Ident); ok && id.Name == field {
					// A nil literal is not wiring — it is the absence of it,
					// written down.
					if gotypes.ExprString(node.Value) == "nil" {
						return true
					}
					sites = append(sites, positionOf(fset, path, node.Pos()))
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("percorrendo %s: %v", dir, err)
	}
	return len(sites) > 0, sites
}

func positionOf(fset *token.FileSet, path string, pos token.Pos) string {
	return filepath.Base(path) + ":" + strings.TrimPrefix(fset.Position(pos).String(), path+":")
}
