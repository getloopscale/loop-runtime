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

// WHAT THIS CHECK DOES NOT CATCH — measured 2026-08-12 by an independent review
// that ran six variants through Check and got err==nil on all six:
//
//	uk := strings.ToUpper(key)     // hoisted ABOVE the range — the one a normal
//	for _, p := range prefixes {   // refactor produces
//	    if strings.HasPrefix(uk, p)
//
//	for i := 0; i < len(prefixes); i++      // index loop, not a range
//	ps := prefixes; for _, p := range ps    // aliased through a variable
//	slices.ContainsFunc(prefixes, ...)       // no loop at all
//	import str "strings"                     // aliased package
//	bytes.ToUpper                            // different package
//
// Closing those needs dataflow on the compared expression, which is a different
// order of work than this gate. Saying so here is the alternative to a green
// that overstates itself — the defect this gate's own package doc warns about.
// Registered as its own item rather than left implicit.
//
// What IS closed is the vacuous pass: a site where no matching construct is
// found at all now fails, because a check that verified nothing must never
// report success (the `PASS — 0 routes` shape of B-040).

// ErrNoMatchingLoop means a site declares the prefixes but no loop over them was
// found — so nothing about its case behaviour was verified.
var ErrNoMatchingLoop = errors.New("no loop over the reserved prefixes was found at a site; " +
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

// checkCaseMatching parses each site and reports every case-folding call that
// appears INSIDE a range statement over the prefix symbol.
//
// Scoping to that loop is what makes the check precise. A fold anywhere else in
// the file is somebody else's business; a fold in the loop that compares the key
// against the reserved prefixes is this rule's semantics.
func checkCaseMatching(repoRoot string, sites []Site) ([]CaseFinding, error) {
	var findings []CaseFinding
	for _, site := range sites {
		sawLoop := false
		abs := filepath.Join(repoRoot, site.RelPath)
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, abs, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", abs, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			rng, ok := n.(*ast.RangeStmt)
			if !ok || !rangesOver(rng, site.Symbol) {
				return true
			}
			sawLoop = true
			ast.Inspect(rng.Body, func(inner ast.Node) bool {
				call, ok := inner.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || pkg.Name != "strings" || !caseFoldFuncs[sel.Sel.Name] {
					return true
				}
				findings = append(findings, CaseFinding{
					File: site.RelPath,
					Call: "strings." + sel.Sel.Name,
					Line: fset.Position(call.Pos()).Line,
				})
				return true
			})
			return true
		})
		if !sawLoop {
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

// rangesOver reports whether the range statement iterates the named symbol.
func rangesOver(rng *ast.RangeStmt, symbol string) bool {
	id, ok := rng.X.(*ast.Ident)
	return ok && id.Name == symbol
}

// FormatCaseFindings renders the findings for the gate output.
func FormatCaseFindings(fs []CaseFinding) string {
	var b strings.Builder
	for _, f := range fs {
		fmt.Fprintf(&b, "  %s:%d — %s inside the loop over the reserved prefixes\n", f.File, f.Line, f.Call)
	}
	return b.String()
}
