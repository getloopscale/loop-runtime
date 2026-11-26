// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package testskipabsentpath

import (
	"go/parser"
	"go/token"
	"testing"
)

// Unit tests of the path resolver on its own, with the package index built from
// a one-file package. resolve() answers the path AS THE EXPRESSION SPELLS IT;
// joining it under the package directory and classifying it as external happen
// later, in newSite. The spelling-by-spelling regressions live in the other test
// files and go through Check.

func resolverFor(t *testing.T, src string) *resolver {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "pkg/consts.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg := indexPackage([]parsedFile{{rel: "pkg/consts.go", fset: fset, ast: f}})
	return &resolver{root: t.TempDir(), pkgDir: "pkg", file: "pkg/x_test.go", pkg: pkg}
}

func TestResolve(t *testing.T) {
	r := resolverFor(t, `package p

import "path/filepath"

const cliDir = "cli/src"

func chartsDir() string { return filepath.Join("..", "charts") }
`)
	cases := []struct {
		expr     string
		wantPath string
		wantKind resolveKind
	}{
		{`"cli/src"`, "cli/src", kindResolved},
		{`filepath.Join("a", "b")`, "a/b", kindResolved},
		{`cliDir`, "cli/src", kindResolved},
		{`filepath.Join(cliDir, "index.ts")`, "cli/src/index.ts", kindResolved},
		{`chartsDir()`, "../charts", kindResolved},
		{`someUnknownName`, "", kindUnresolved},
		{`42`, "", kindUnresolved},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			e, err := parser.ParseExpr(c.expr)
			if err != nil {
				t.Fatalf("parse %q: %v", c.expr, err)
			}

			path, kind := r.resolve(e, r.file, 0)

			if kind != c.wantKind || (c.wantKind != kindUnresolved && path != c.wantPath) {
				t.Errorf("resolve(%s) = (%q, %v), want (%q, %v)", c.expr, path, kind, c.wantPath, c.wantKind)
			}
		})
	}
}

func TestResolve_StopsAtTheDepthBound(t *testing.T) {
	r := resolverFor(t, "package p\n")
	e, err := parser.ParseExpr(`"cli/src"`)
	if err != nil {
		t.Fatal(err)
	}

	if _, kind := r.resolve(e, r.file, maxResolveDepth+1); kind != kindUnresolved {
		t.Fatalf("kind = %v past the depth bound, want kindUnresolved", kind)
	}
}
