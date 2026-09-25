// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package adapterports gives the `adapters` package an admission criterion that can refuse.
//
// # The defect, measured
//
// B-058, 2026-08-18. The `api/internal/adapters` package admitted members by a rule that
// denies nothing: *"it is an adapter"*. Measured: of the **nine** adapters, five named the
// port they satisfy in **prose**, four said nothing, and only **one** asserted it at compile
// time.
//
// Prose does not compile. A comment saying *"implements routes.X"* stays there after the
// interface changes shape, name or location — and the next reader trusts it.
//
// # The criterion
//
// Every file with an exported type in this package declares, with `var _ Port = (*Type)(nil)`,
// which port it satisfies. Whoever cannot name the port does not belong here — and that is
// exactly the sentence the old criterion could not say.
//
// The distinction that matters: `var _ = pkg.Type{}` is a **usage** assertion, not a port
// assertion. It does not claim the adapter satisfies any interface, and confusing the two
// would let through the defect this guard exists to catch.
//
// # The limit
//
// It requires the port to be DECLARED, not that it be the right port. An adapter that
// asserts the wrong interface compiles and passes here — but the consumer would not
// compile, so the compiler covers that half.
package adapterports

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoPortDeclared means an adapter file declares an exported type without naming its port.
var ErrNoPortDeclared = errors.New("adapter does not declare the port it satisfies")

// ErrNothingScanned means the package is absent — silence, not cleanliness.
//
// The other alternative in B-058's DoD is to DISSOLVE this package. If that happens, this
// guard must fail instead of staying green over a directory that disappeared.
var ErrNothingScanned = errors.New("adapters package absent — finding nothing is not the same as finding nothing wrong")

const adaptersPackage = "api/internal/adapters"

// Config drives the check.
type Config struct {
	RepoRoot string
}

// Outcome reports what was established.
type Outcome struct {
	Scanned int
	Missing []string
}

// declaresPort reports whether the file has `var _ <something> = ...` with a declared TYPE.
//
// `var _ = expr` (no type) is a usage assertion and does not count.
func declaresPort(file *ast.File) bool {
	for _, d := range file.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.VAR {
			continue
		}
		for _, spec := range g.Specs {
			v, ok := spec.(*ast.ValueSpec)
			if !ok || v.Type == nil {
				continue // no declared type: usage assertion
			}
			for _, n := range v.Names {
				if n.Name == "_" {
					return true
				}
			}
		}
	}
	return false
}

// hasExportedType reports whether the file declares any exported type.
func hasExportedType(file *ast.File) bool {
	for _, d := range file.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.TYPE {
			continue
		}
		for _, spec := range g.Specs {
			if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.IsExported() {
				return true
			}
		}
	}
	return false
}

// Check verifies every adapter declares the port it satisfies.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	dir := filepath.Join(root, filepath.FromSlash(adaptersPackage))

	entries, err := os.ReadDir(dir)
	if err != nil {
		return out, fmt.Errorf("%w: %s", ErrNothingScanned, adaptersPackage)
	}

	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			return out, fmt.Errorf("parse %s: %w", name, err)
		}
		if !hasExportedType(file) {
			continue // not an adapter; there is no port to require
		}
		out.Scanned++
		if !declaresPort(file) {
			out.Missing = append(out.Missing, name)
		}
	}

	if out.Scanned == 0 && len(out.Missing) == 0 {
		// No file with an exported type: either the package was dissolved, or the scan
		// reached nothing. Both deserve someone's eyes.
		return out, fmt.Errorf("%w: %s has no adapter at all", ErrNothingScanned, adaptersPackage)
	}
	if len(out.Missing) > 0 {
		return out, fmt.Errorf("%w: %s — declare `var _ <Port> = (*<Type>)(nil)`",
			ErrNoPortDeclared, strings.Join(out.Missing, ", "))
	}
	return out, nil
}
