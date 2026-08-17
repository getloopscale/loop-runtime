// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package envprefixparity

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
)

// The CASE axis of the same rule (B-070, ADR 2026-08-12-reserved-env-case-matching).
//
// The prefix SETS matching proves the two copies reserve the same names. It says
// nothing about how a candidate key is COMPARED against them, and the two copies
// disagreed there for as long as both existed: the API compared exactly, the
// linter upper-cased the key first. A build with `ENV aws_region` was rejected
// by one side and accepted by the other.
//
// Environment variables are case-sensitive on Linux and every name LoopZero injects
// is uppercase, so a lowercase key cannot shadow anything — the fold rejected
// builds over a collision the OS does not permit. The contract is now: compare
// the key as written.
//
// Why this is asserted structurally rather than by grepping for "ToUpper": the
// SAME file legitimately upper-cases elsewhere. `containsReservedEnvKey` folds
// the INSTRUCTION keyword (`ARG `/`ENV `) because Dockerfile instructions are
// case-insensitive by specification — one line above the key comparison. A text
// scan cannot tell the two apart; an AST scoped to the loop over the prefix
// slice can, and does.

// # A asserção acompanha a EXPRESSÃO COMPARADA, não a forma do laço (B-090)
//
// A primeira versão escopava a busca a um `*ast.RangeStmt` cujo `X` era o identificador
// do símbolo. Uma revisão independente rodou seis variantes contra ela em 2026-08-12 e
// **todas as seis passaram** — bastava mudar a forma do laço, ou tirar o laço:
//
//	uk := strings.ToUpper(key)     // fold içado ACIMA do range — a que um refactor normal produz
//	for i := 0; i < len(p); i++    // laço por índice, não um RangeStmt
//	ps := prefixes; for _, p := range ps   // aliasado por variável
//	slices.ContainsFunc(prefixes, ...)     // sem laço nenhum
//	import str "strings"                   // pacote com alias
//	bytes.ToUpper                          // outro pacote
//
// O escopo agora é a FUNÇÃO que compara, e o que se procura é uma **comparação contra
// um prefixo cujo outro lado passou por um fold** — direto (`HasPrefix(ToUpper(k), p)`)
// ou por uma atribuição (`uk := ToUpper(k)` … `HasPrefix(uk, p)`).
//
// O discriminador que mantém o fold LEGÍTIMO verde é o outro argumento. A mesma função
// dobra a caixa da palavra da instrução do Dockerfile (`ENV `/`ARG `), porque a
// especificação a torna insensível a caixa — e isso é uma comparação contra um
// **literal**. Comparar contra a variável do laço sobre os prefixos é a chave; comparar
// contra um literal é a instrução.
//
// O que continua fora: um fold escondido atrás de uma função própria
// (`normalize(key)`), que exigiria seguir a chamada. Dizê-lo aqui é a alternativa a um
// verde que se superestima.
//
// O passo vazio segue fechado: um site sem NENHUMA comparação contra os prefixos
// reprova, porque uma verificação que não verificou nada nunca pode reportar sucesso
// (a forma `PASS — 0 routes` do B-040).

// ErrNoMatchingLoop means a site declares the prefixes but no loop over them was
// found — so nothing about its case behaviour was verified.
var ErrNoMatchingLoop = errors.New("no comparison against the reserved prefixes was found at a site; " +
	"the case axis was not verified there")

// ErrCaseFoldingInMatch reports a site that folds case while comparing a key
// against the reserved prefixes.
var ErrCaseFoldingInMatch = errors.New("a reserved-prefix comparison folds case; " +
	"matching is exact per ADR 2026-08-12-reserved-env-case-matching")

// caseFoldFuncs are the standard-library calls that fold case.
var caseFoldFuncs = map[string]bool{
	"ToUpper":     true,
	"ToLower":     true,
	"EqualFold":   true,
	"HasPrefix":   false, // listed to make the intent explicit: HasPrefix itself is fine
	"ToValidUTF8": false,
}

// CaseFinding names a site that folds case inside the prefix-matching loop.
type CaseFinding struct {
	File string // repo-relative
	Call string // e.g. "strings.ToUpper"
	Line int
}

// checkCaseMatching parses each site and reports every prefix comparison whose
// compared expression passed through a case fold.
func checkCaseMatching(repoRoot string, sites []Site) ([]CaseFinding, error) {
	var findings []CaseFinding
	for _, site := range sites {
		abs := filepath.Join(repoRoot, site.RelPath)
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, abs, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", abs, err)
		}
		sawComparison := false
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !mentions(fn.Body, site.Symbol) {
				continue
			}
			// Uma passada colhe os identificadores que RECEBEM um fold; a seguinte
			// procura as comparações. Duas passadas porque a atribuição pode vir
			// depois do uso em ordem de escrita, e a de cima é a variante içada.
			folded := foldedIdents(fn.Body)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				// Duas formas de comparar, e o site REAL usa a segunda: a chamada
				// (`HasPrefix(k, p)`) e o binário com fatiamento
				// (`k[:len(p)] == p`). A primeira versão só conhecia a chamada, e a
				// árvore real não tem nenhuma — o gate teria reprovado o próprio
				// repositório por "não achei comparação".
				var compared, against ast.Expr
				switch v := n.(type) {
				case *ast.CallExpr:
					if !isComparisonFunc(v) || len(v.Args) < 2 {
						return true
					}
					compared, against = v.Args[0], v.Args[1]
				case *ast.BinaryExpr:
					if v.Op != token.EQL && v.Op != token.NEQ {
						return true
					}
					compared, against = v.X, v.Y
				default:
					return true
				}
				// O outro lado precisa ser o prefixo — não um literal. Literal é a
				// palavra da instrução do Dockerfile, cujo fold é correto por
				// especificação.
				if !refersToPrefix(against, site.Symbol, fn) {
					return true
				}
				sawComparison = true
				if name, line, ok := foldIn(compared, folded, fset); ok {
					findings = append(findings, CaseFinding{File: site.RelPath, Call: name, Line: line})
				}
				return true
			})
		}
		if !sawComparison {
			return nil, fmt.Errorf("%w: %s", ErrNoMatchingLoop, site.RelPath)
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, nil
}

// mentions reports whether a block references the named symbol.
func mentions(block *ast.BlockStmt, symbol string) bool {
	found := false
	ast.Inspect(block, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == symbol {
			found = true
		}
		return true
	})
	return found
}

// isComparisonFunc reports whether a call is a prefix/equality comparison.
//
// Casa pelo NOME do seletor, não pelo pacote: `str.ToUpper` e `bytes.ToUpper` dobram
// a caixa tanto quanto `strings.ToUpper`, e exigir o pacote foi como duas das seis
// variantes escaparam.
func isComparisonFunc(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "HasPrefix", "EqualFold", "Contains", "HasSuffix":
		return true
	}
	return false
}

// refersToPrefix reports whether an expression is the prefix element being compared —
// the loop variable over the symbol, an index into it, or a closure parameter.
func refersToPrefix(e ast.Expr, symbol string, fn *ast.FuncDecl) bool {
	switch v := e.(type) {
	case *ast.IndexExpr:
		id, ok := v.X.(*ast.Ident)
		return ok && (id.Name == symbol || aliasesSymbol(fn, id.Name, symbol))
	case *ast.Ident:
		// Um literal nunca chega aqui, e é essa a fronteira que mantém o fold da
		// palavra da instrução verde.
		return isLoopVarOver(fn, v.Name, symbol) || isClosureParam(fn, v.Name)
	}
	return false
}

// isLoopVarOver reports whether name is bound by a range over symbol (or an alias).
func isLoopVarOver(fn *ast.FuncDecl, name, symbol string) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		rng, ok := n.(*ast.RangeStmt)
		if !ok || rng.Value == nil {
			return true
		}
		id, ok := rng.Value.(*ast.Ident)
		if !ok || id.Name != name {
			return true
		}
		x, ok := rng.X.(*ast.Ident)
		if ok && (x.Name == symbol || aliasesSymbol(fn, x.Name, symbol)) {
			found = true
		}
		return true
	})
	return found
}

// aliasesSymbol reports whether name was assigned from symbol — `ps := prefixes`.
func aliasesSymbol(fn *ast.FuncDecl, name, symbol string) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		lhs, ok := as.Lhs[0].(*ast.Ident)
		if !ok || lhs.Name != name {
			return true
		}
		if rhs, ok := as.Rhs[0].(*ast.Ident); ok && rhs.Name == symbol {
			found = true
		}
		return true
	})
	return found
}

// isClosureParam reports whether name is a parameter of some function literal in the
// body — the `slices.ContainsFunc(prefixes, func(p string) bool {…})` form.
func isClosureParam(fn *ast.FuncDecl, name string) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if !ok || lit.Type.Params == nil {
			return true
		}
		for _, field := range lit.Type.Params.List {
			for _, id := range field.Names {
				if id.Name == name {
					found = true
				}
			}
		}
		return true
	})
	return found
}

// foldedIdents maps an identifier to the fold call that produced it.
func foldedIdents(block *ast.BlockStmt) map[string]*ast.CallExpr {
	out := map[string]*ast.CallExpr{}
	ast.Inspect(block, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		lhs, ok := as.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		if call, ok := as.Rhs[0].(*ast.CallExpr); ok && isFoldCall(call) != "" {
			out[lhs.Name] = call
		}
		return true
	})
	return out
}

// isFoldCall returns the rendered name when a call folds case, else "".
func isFoldCall(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !caseFoldFuncs[sel.Sel.Name] {
		return ""
	}
	if pkg, ok := sel.X.(*ast.Ident); ok {
		return pkg.Name + "." + sel.Sel.Name
	}
	return sel.Sel.Name
}

// foldIn reports whether the compared expression passed through a fold — inline, or
// through one assignment (the hoisted variant).
func foldIn(e ast.Expr, folded map[string]*ast.CallExpr, fset *token.FileSet) (string, int, bool) {
	var name string
	var pos token.Pos
	ast.Inspect(e, func(n ast.Node) bool {
		if name != "" {
			return false
		}
		switch v := n.(type) {
		case *ast.CallExpr:
			if got := isFoldCall(v); got != "" {
				name, pos = got, v.Pos()
			}
		case *ast.Ident:
			if call, ok := folded[v.Name]; ok {
				name, pos = isFoldCall(call), call.Pos()
			}
		}
		return true
	})
	if name == "" {
		return "", 0, false
	}
	return name, fset.Position(pos).Line, true
}

// FormatCaseFindings renders the findings for the gate output.
func FormatCaseFindings(fs []CaseFinding) string {
	var b strings.Builder
	for _, f := range fs {
		fmt.Fprintf(&b, "  %s:%d — %s inside the loop over the reserved prefixes\n", f.File, f.Line, f.Call)
	}
	return b.String()
}
