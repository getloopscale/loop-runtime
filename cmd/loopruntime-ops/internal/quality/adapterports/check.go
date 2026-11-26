// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package adapterports gives the adapter packages an admission criterion that can refuse.
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
// Every file with an exported type in an adapter package (`api/internal/<feature>/<feature>adapter`,
// where the adapters package's members went when api/internal was organized by feature)
// declares, with `var _ Port = (*Type)(nil)`, which port it satisfies. Whoever cannot name the port does not belong here — and that is
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

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/scanresult"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

// ErrNoPortDeclared means an adapter file declares an exported type without naming its port.
var ErrNoPortDeclared = errors.New("adapter does not declare the port it satisfies")

// ErrNothingScanned means no adapter package exists — silence, not cleanliness.
//
// The other alternative in B-058's DoD was to DISSOLVE the adapters package. That
// happened when api/internal was organized by feature (docs/decisions.md,
// 2026-09-25): each adapter moved to `<feature>/<feature>adapter`. This guard
// followed them, and fails instead of staying green over directories that
// disappeared.
var ErrNothingScanned = fmt.Errorf("%w: no adapter package", scanresult.ErrNothingScanned)

// adapterPackagesGlob matches every feature's adapter package: a direct child of
// a top-level api/internal directory whose name ends in "adapter" (feature-fence
// keeps them there and named `<feature>adapter`).
const adapterPackagesGlob = "api/internal/*/*adapter"

// Config drives the check.
type Config struct {
	RepoRoot string
}

// Outcome reports what was established.
type Outcome struct {
	Scanned int
	Missing []string
	// Violation is what the check found wrong (wrapping one of its Err*
	// reasons), or nil. A violation is the check's verdict, never its error.
	Violation error
}

// Verdict is Fail when the check found a violation and Pass otherwise.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Violation != nil {
		return verdict.Fail
	}
	return verdict.Pass
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
	dirs, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(adapterPackagesGlob)))
	if err != nil {
		return out, fmt.Errorf("adapter-ports: %w", err)
	}

	fset := token.NewFileSet()
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // a file matching the glob, not a package directory
		}
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return out, err
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
			if err != nil {
				return out, fmt.Errorf("parse %s: %w", filepath.Join(rel, name), err)
			}
			if !hasExportedType(file) {
				continue // not an adapter; there is no port to require
			}
			out.Scanned++
			if !declaresPort(file) {
				out.Missing = append(out.Missing, filepath.ToSlash(filepath.Join(rel, name)))
			}
		}
	}

	if out.Scanned == 0 && len(out.Missing) == 0 {
		// No file with an exported type: either the package was dissolved, or the scan
		// reached nothing. Both deserve someone's eyes.
		return out, fmt.Errorf("%w: %s holds no adapter at all", ErrNothingScanned, adapterPackagesGlob)
	}
	if len(out.Missing) > 0 {
		out.Violation = fmt.Errorf("%w: %s — declare `var _ <Port> = (*<Type>)(nil)`",
			ErrNoPortDeclared, strings.Join(out.Missing, ", "))
		return out, nil
	}
	return out, nil
}
