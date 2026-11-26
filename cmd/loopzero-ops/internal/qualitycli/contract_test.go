// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// The gate contract (docs/decisions.md, "One verdict contract for the quality
// gates"): a gate's check returns its own outcome, which states its verdict
// through verdict.Result, and the runner alone turns that verdict into an exit
// status. A gate adapter renders the outcome; it never decides the verdict.
//
// Two things hold that in place. verdict.Verdict does not implement
// verdict.Result, so a Check cannot return a verdict it chose
// (TestAVerdictIsNotAResult); and no file in this package may declare a
// Verdict method, so an adapter cannot wrap a hand-picked verdict in a type of
// its own (TestNoAdapterDeclaresItsOwnVerdict).

type adapterFile struct {
	path  string
	file  *ast.File
	gates []string
}

func parseAdapters(t *testing.T) []adapterFile {
	t.Helper()
	fset := token.NewFileSet()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var out []adapterFile
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		out = append(out, adapterFile{path: p, file: f, gates: registeredNames(f)})
	}
	return out
}

// registeredNames returns the Name of every Gate literal in f.
func registeredNames(f *ast.File) []string {
	var names []string
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if id, ok := lit.Type.(*ast.Ident); !ok || id.Name != "Gate" {
			return true
		}
		for _, e := range lit.Elts {
			kv, ok := e.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Name" {
				if bl, ok := kv.Value.(*ast.BasicLit); ok {
					if s, err := strconv.Unquote(bl.Value); err == nil {
						names = append(names, s)
					}
				}
			}
		}
		return true
	})
	return names
}

func TestEveryRegisteredGateHasOneAdapterFile(t *testing.T) {
	seen := map[string]string{}
	for _, af := range parseAdapters(t) {
		for _, g := range af.gates {
			seen[g] = af.path
		}
	}
	for _, g := range Gates() {
		if _, ok := seen[g.Name]; !ok {
			t.Errorf("gate %q is registered but no adapter file declares it by a literal name — the contract check cannot see it", g.Name)
		}
	}
}

// TestNoAdapterDeclaresItsOwnVerdict refuses a Result implemented inside this
// package: the verdict is stated by a check's own package, next to the data it
// reads, and an adapter-local Result would be a verdict chosen by hand under
// another name.
func TestNoAdapterDeclaresItsOwnVerdict(t *testing.T) {
	for _, af := range parseAdapters(t) {
		for _, d := range af.file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Name.Name != "Verdict" {
				continue
			}
			t.Errorf("%s declares a Verdict method on %s: a check's package states the verdict, not its adapter",
				af.path, recvName(fd))
		}
	}
}

func recvName(fd *ast.FuncDecl) string {
	switch x := fd.Recv.List[0].Type.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return "?"
}

func TestContractCheckSeesTheRegistry(t *testing.T) {
	// The file walk runs from the package directory; if it ever stopped seeing
	// the adapters, every gate would read as migrated.
	if _, err := os.Stat("gate.go"); err != nil {
		t.Fatalf("the contract check must run from the qualitycli directory: %v", err)
	}
	n := 0
	for _, af := range parseAdapters(t) {
		n += len(af.gates)
	}
	if n != len(Gates()) {
		t.Fatalf("adapter files declare %d gate(s), the registry holds %d", n, len(Gates()))
	}
}

// TestAVerdictIsNotAResult is what keeps a Check from returning a verdict
// constant: the runner takes a verdict.Result, and a bare verdict is not one.
func TestAVerdictIsNotAResult(t *testing.T) {
	result := reflect.TypeOf((*verdict.Result)(nil)).Elem()

	if reflect.TypeOf(verdict.Pass).Implements(result) {
		t.Fatal("verdict.Verdict implements verdict.Result: an adapter could return a verdict it chose")
	}
}
