// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package sqlctxprop enforces that sqlc-generated calls inside a
// `withEnvironmentLock` (or equivalent transactional) block use the
// tx-bound `Queries.WithTx(tx).<method>(...)` form rather than the
// pool-bound form `Queries{}.<method>(...)`.
//
// T0.4 of plan 2026-05-29-sqlc-migration-plan v1.2.
//
// Why this gate exists (per MEET-2026-05-29-001 distributed-systems-engineer
// R-DS-1):
//
//	The sqlc-generated Queries struct has BOTH a pool-bound form (executes
//	against the connection pool) and a tx-bound form (`WithTx(tx)` clone
//	that executes inside the caller's transaction). If a caller uses the
//	pool-bound form INSIDE a `withEnvironmentLock` block, the inner call
//	may execute on a DIFFERENT connection than the lock holder — silent
//	lock violation. Rule 6 + Rule 7 invariant breach.
//
// Phase 0 baseline behavior: no sqlc-generated imports exist anywhere
// in api/internal/{routes,services} yet, so the gate trivially passes.
// First real exercise lands when Phase 1 stores convert.
package sqlctxprop

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// Config controls the gate.
type Config struct {
	// RepoRoot is the repository root. Default ".".
	RepoRoot string
}

// Violation captures a sqlc-call-inside-lock without WithTx threading.
type Violation struct {
	File     string
	Line     int
	Function string
	Detail   string
}

// Outcome is what the gate returns.
type Outcome struct {
	Pass       bool
	Violations []Violation
	Scanned    int // count of .go files scanned
}

const (
	defaultRoutesDir    = "api/internal/routes"
	defaultServicesDir  = "api/internal/services"
	lockSentinel        = "withEnvironmentLock"
	generatedImportPath = "github.com/getloopscale/loopzero/api/internal/db/generated"
)

// Check is the gate entry point.
func Check(cfg Config) (Outcome, error) {
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}

	var allViolations []Violation
	scanned := 0

	for _, sub := range []string{defaultRoutesDir, defaultServicesDir} {
		dir := filepath.Join(cfg.RepoRoot, sub)
		if _, err := os.Stat(dir); err != nil {
			// Dir not present is acceptable on early phases.
			continue
		}
		fileViolations, fileCount, err := walkDir(dir)
		if err != nil {
			return Outcome{}, err
		}
		allViolations = append(allViolations, fileViolations...)
		scanned += fileCount
	}

	return Outcome{
		Pass:       len(allViolations) == 0,
		Violations: allViolations,
		Scanned:    scanned,
	}, nil
}

// ActionableFix returns the operator-facing message for FAIL output.
func ActionableFix() string {
	return "Replace `<store>.MethodName(...)` (pool-bound) with " +
		"`<store>.WithTx(tx).MethodName(...)` (tx-bound) inside any " +
		"`withEnvironmentLock` block. Rule 6 + Rule 7 invariant " +
		"requires the lock holder's transaction to be threaded through " +
		"every nested sqlc-generated call."
}

// walkDir walks dir for .go files (excluding *_test.go), parses each,
// and collects violations.
func walkDir(dir string) ([]Violation, int, error) {
	var violations []Violation
	count := 0

	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".go") {
			return nil
		}
		if strings.HasSuffix(info.Name(), "_test.go") {
			return nil
		}

		count++

		fileViolations, err := scanFile(path)
		if err != nil {
			return err
		}
		violations = append(violations, fileViolations...)
		return nil
	})

	return violations, count, err
}

// scanFile parses a single .go file and returns violations found inside it.
//
// Algorithm:
//  1. Parse file via go/parser.
//  2. Check if file imports the sqlc-generated package (`api/internal/db/generated`).
//     If NOT, file cannot have sqlc-generated calls — skip (zero violations).
//  3. Walk AST top-down: find any CallExpr where the callee is
//     `withEnvironmentLock`. For each, inspect the function-literal argument's
//     body for nested CallExpr that look like sqlc-generated calls.
//  4. A sqlc-generated call WITHOUT `.WithTx(...)` chain is a violation.
//
// Phase 0 baseline: no file imports generated/ yet, so all returns are empty.
func scanFile(path string) ([]Violation, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		// Tolerate parse errors on files that may be mid-flight; surface
		// upstream by returning error.
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if !fileImportsGenerated(file) {
		return nil, nil
	}

	var violations []Violation

	// Walk for withEnvironmentLock call sites.
	ast.Inspect(file, func(n ast.Node) bool {
		callExpr, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if !isWithEnvironmentLockCall(callExpr) {
			return true
		}
		// Inspect the function-literal closure body for nested sqlc calls.
		bodyViolations := scanLockClosure(callExpr, fset, path)
		violations = append(violations, bodyViolations...)
		// Continue walking; nested locks (unlikely) still get scanned.
		return true
	})

	return violations, nil
}

// fileImportsGenerated returns true if the file imports the sqlc-generated
// package. Phase 0 baseline: returns false for all files.
func fileImportsGenerated(file *ast.File) bool {
	for _, imp := range file.Imports {
		if imp.Path == nil {
			continue
		}
		// Strip quotes from import path literal.
		path := strings.Trim(imp.Path.Value, `"`)
		if path == generatedImportPath {
			return true
		}
	}
	return false
}

// isWithEnvironmentLockCall returns true if the CallExpr's callee name
// resolves to `withEnvironmentLock` (or `WithEnvironmentLock` for exported).
func isWithEnvironmentLockCall(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name == lockSentinel || fn.Name == "WithEnvironmentLock"
	case *ast.SelectorExpr:
		return fn.Sel != nil && (fn.Sel.Name == lockSentinel || fn.Sel.Name == "WithEnvironmentLock")
	}
	return false
}

// scanLockClosure inspects the function-literal closure argument of a
// withEnvironmentLock call. For each call expression nested inside that
// closure body, verifies that ANY identifier matching `Queries` or `*Queries`
// has been through a `.WithTx(...)` chain.
//
// Heuristic: any call site `<x>.<Method>(...)` where `<x>` is a Queries-like
// receiver AND the call is NOT preceded by `.WithTx(...)` is a violation.
func scanLockClosure(call *ast.CallExpr, fset *token.FileSet, path string) []Violation {
	var violations []Violation

	// The last argument to withEnvironmentLock is typically the func closure.
	if len(call.Args) == 0 {
		return nil
	}

	for _, arg := range call.Args {
		funcLit, ok := arg.(*ast.FuncLit)
		if !ok {
			continue
		}
		if funcLit.Body == nil {
			continue
		}

		ast.Inspect(funcLit.Body, func(n ast.Node) bool {
			innerCall, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if !looksLikeSqlcCall(innerCall) {
				return true
			}
			if hasWithTxChain(innerCall) {
				return true
			}

			pos := fset.Position(innerCall.Pos())
			violations = append(violations, Violation{
				File:     path,
				Line:     pos.Line,
				Function: callExprString(innerCall),
				Detail:   "sqlc-generated call inside withEnvironmentLock closure WITHOUT .WithTx(tx) threading",
			})
			return true
		})
	}

	return violations
}

// looksLikeSqlcCall heuristically detects calls against the sqlc-generated
// Queries type. Since AST cannot resolve types without go/types, we use
// the convention `<storeIdentifier>.<MethodName>(...)` where the method name
// begins with a capital letter (Go exported). Phase 0 baseline: never matches.
//
// Refinement opportunity (T1.1+): once first store conversion lands, this
// heuristic will be calibrated against real callsites; the gate can switch
// to go/types if needed.
func looksLikeSqlcCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel == nil {
		return false
	}
	// Method name must start with a capital letter (Go exported).
	name := sel.Sel.Name
	if name == "" {
		return false
	}
	if name[0] < 'A' || name[0] > 'Z' {
		return false
	}
	// `WithTx` is the threading helper itself — not a sqlc method.
	if name == "WithTx" {
		return false
	}
	// Receiver may be: Ident (`queries.Foo`), SelectorExpr
	// (`s.queries.Foo`), or CallExpr (`s.queries.WithTx(tx).Foo` — but
	// that's the GOOD pattern handled by hasWithTxChain). Accept ident
	// and selector-expr receivers as candidate sqlc calls.
	switch sel.X.(type) {
	case *ast.Ident, *ast.SelectorExpr:
		return true
	}
	return false
}

// hasWithTxChain returns true if the call expression is part of a
// `.WithTx(tx).<Method>(...)` chain.
func hasWithTxChain(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	// Walk back: sel.X may itself be a CallExpr to `.WithTx(...)`.
	if innerCall, ok := sel.X.(*ast.CallExpr); ok {
		if innerSel, ok := innerCall.Fun.(*ast.SelectorExpr); ok && innerSel.Sel != nil {
			return innerSel.Sel.Name == "WithTx"
		}
	}
	return false
}

// callExprString returns a short human-readable representation of a call site.
func callExprString(call *ast.CallExpr) string {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		recv := ""
		if id, ok := sel.X.(*ast.Ident); ok {
			recv = id.Name
		}
		if sel.Sel != nil {
			return recv + "." + sel.Sel.Name + "(...)"
		}
	}
	return "<call>"
}
