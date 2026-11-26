// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package sqlctxprop enforces that a sqlc-backed store function which received
// the caller's transaction runs every query on that transaction.
//
// Why (MEET-2026-05-29-001, R-DS-1): a query run on the connection pool inside a
// transactional path executes on a different connection from the lock holder —
// a silent violation of the environment lock (Rules 6 and 7).
//
// # The shape this gate reads
//
// sqlc generates methods that take the database handle per call
// (`q.Method(ctx, dbtx, ...)`), and the stores in api/internal/db/store pick it
// with `s.gen(tx)`, where `s.gen(nil)` means the pool. So the invariant is
// concrete: inside a function that has a `db.DBTX` parameter, no call may pass
// `s.gen(nil)` or the store's `pool` field.
//
// An earlier version of this gate scanned api/internal/routes and
// api/internal/services for `WithTx`. Neither may import the generated package
// at all (db-generated-boundary forbids it), and the generated code has no
// WithTx, so it could never fire. The scope and the rule now follow the code.
//
// # Limits
//
// The check is syntactic: a DBTX parameter marks a transactional function, and a
// transaction passed on to a helper that then uses the pool is not followed.
package sqlctxprop

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

// Config controls the gate.
type Config struct {
	// RepoRoot is the repository root. Default ".".
	RepoRoot string
}

// Violation is a pool-bound query inside a function that received a transaction.
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
	// Scanned counts the non-test files importing the generated package.
	Scanned int
	// TxFunctions counts the functions with a db.DBTX parameter that were checked.
	TxFunctions int
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// ErrNothingScanned means the scan met no transactional function: the rule was
// checked against nothing.
var ErrNothingScanned = errors.New("no sqlc-backed function with a db.DBTX parameter was found — the gate checked nothing")

const (
	defaultStoreDir     = "api/internal/db/store"
	generatedImportPath = "github.com/getloopscale/loop-runtime/api/internal/db/generated"
)

// Check is the gate entry point.
func Check(cfg Config) (Outcome, error) {
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	dir := filepath.Join(cfg.RepoRoot, defaultStoreDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Outcome{}, fmt.Errorf("reading %s: %w", defaultStoreDir, err)
	}

	var out Outcome
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return Outcome{}, fmt.Errorf("parsing %s: %w", path, err)
		}
		if !importsGenerated(file) {
			continue
		}
		out.Scanned++
		rel := filepath.ToSlash(filepath.Join(defaultStoreDir, name))
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || !hasDBTXParam(fd) {
				continue
			}
			out.TxFunctions++
			out.Violations = append(out.Violations, poolCalls(fset, rel, fd)...)
		}
	}
	if out.TxFunctions == 0 {
		return out, ErrNothingScanned
	}
	out.Pass = len(out.Violations) == 0
	return out, nil
}

func importsGenerated(f *ast.File) bool {
	for _, imp := range f.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err == nil && p == generatedImportPath {
			return true
		}
	}
	return false
}

// hasDBTXParam reports whether fd takes a parameter of type DBTX (qualified or not).
func hasDBTXParam(fd *ast.FuncDecl) bool {
	for _, field := range fd.Type.Params.List {
		switch t := field.Type.(type) {
		case *ast.SelectorExpr:
			if t.Sel.Name == "DBTX" {
				return true
			}
		case *ast.Ident:
			if t.Name == "DBTX" {
				return true
			}
		}
	}
	return false
}

// poolCalls reports each call in fd's body that hands the pool to a query:
// `<x>.gen(nil)` or an argument `<x>.pool`.
func poolCalls(fset *token.FileSet, file string, fd *ast.FuncDecl) []Violation {
	var out []Violation
	report := func(pos token.Pos, detail string) {
		out = append(out, Violation{File: file, Line: fset.Position(pos).Line, Function: fd.Name.Name, Detail: detail})
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "gen" &&
			len(call.Args) == 1 && isNil(call.Args[0]) {
			report(call.Pos(), "gen(nil) selects the connection pool inside a function that received a transaction")
			return true
		}
		for _, arg := range call.Args {
			if sel, ok := arg.(*ast.SelectorExpr); ok && sel.Sel.Name == "pool" {
				report(arg.Pos(), "the store's pool is passed to a query inside a function that received a transaction")
			}
		}
		return true
	})
	return out
}

func isNil(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "nil"
}

// ActionableFix returns the operator-facing remediation.
func ActionableFix() string {
	return "Pass the transaction: `s.gen(tx)` instead of `s.gen(nil)` or `s.pool`. " +
		"A function that received a db.DBTX runs inside the caller's transaction; a query on the pool " +
		"executes on another connection, outside withEnvironmentLock. If the query genuinely belongs " +
		"outside the transaction, move it to a function that does not take one."
}
