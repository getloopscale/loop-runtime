// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The gate contract (docs/decisions.md, "One verdict contract for the quality
// gates"): a gate's check returns its own outcome, which states its verdict
// through verdict.Result, and the runner alone turns that verdict into an exit
// status. A gate adapter renders the outcome; it never decides the verdict.
//
// A gate that has not moved yet still decides by hand, through the legacy
// Outcome (Pass/Fail), isViolation or errNotVerified. Those gates are listed
// here EXACTLY: a new gate cannot join the list, and a migrated gate must leave
// it, so the list only shrinks.
var legacyOutcomeGates = []string{}

// legacyMarkers are the package-level identifiers through which an adapter
// decides a verdict by hand.
var legacyMarkers = map[string]bool{
	"Pass": true, "Fail": true, "Outcome": true,
	"isViolation": true, "errNotVerified": true,
}

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

// usesAny reports whether node references, unqualified, any identifier in set.
func usesAny(node ast.Node, set map[string]bool) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncDecl:
			// A declaration's own name is not a use of it.
			found = found || usesAny(x.Type, set) || (x.Body != nil && usesAny(x.Body, set))
			return false
		case *ast.KeyValueExpr:
			// Outcome{Pass: ...} names a field, not the legacy constant.
			found = found || usesAny(x.Value, set)
			return false
		case *ast.Field:
			// A struct field or parameter named Pass is not the constant either.
			found = found || usesAny(x.Type, set)
			return false
		case *ast.SelectorExpr:
			// pkg.Pass or out.Pass is not the legacy constant; skip the Sel.
			ast.Inspect(x.X, func(m ast.Node) bool {
				if id, ok := m.(*ast.Ident); ok && set[id.Name] {
					found = true
				}
				return !found
			})
			return false
		case *ast.Ident:
			if set[x.Name] {
				found = true
			}
		}
		return !found
	})
	return found
}

// legacyGates returns the gates whose adapter decides a verdict by hand,
// directly or through a package-level helper that does.
func legacyGates(files []adapterFile) []string {
	markers := map[string]bool{}
	for k := range legacyMarkers {
		markers[k] = true
	}
	// A helper that uses a marker is itself a marker, to a fixed point.
	for changed := true; changed; {
		changed = false
		for _, af := range files {
			if af.path == "gate.go" {
				continue
			}
			for _, d := range af.file.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Recv != nil || fd.Name.Name == "init" || markers[fd.Name.Name] {
					continue
				}
				if usesAny(fd.Body, markers) {
					markers[fd.Name.Name] = true
					changed = true
				}
			}
		}
	}
	var legacy []string
	for _, af := range files {
		if len(af.gates) == 0 {
			continue
		}
		if usesAny(af.file, markers) {
			legacy = append(legacy, af.gates...)
		}
	}
	sort.Strings(legacy)
	return legacy
}

func TestLegacyOutcomeGatesIsExact(t *testing.T) {
	got := legacyGates(parseAdapters(t))
	want := append([]string(nil), legacyOutcomeGates...)
	sort.Strings(want)

	gotSet, wantSet := map[string]bool{}, map[string]bool{}
	for _, g := range got {
		gotSet[g] = true
	}
	for _, g := range want {
		wantSet[g] = true
	}
	for _, g := range got {
		if !wantSet[g] {
			t.Errorf("gate %q decides its verdict by hand (legacy Outcome, isViolation or errNotVerified): "+
				"return its check's outcome, which states the verdict through verdict.Result", g)
		}
	}
	for _, g := range want {
		if !gotSet[g] {
			t.Errorf("gate %q no longer decides its verdict by hand: remove it from legacyOutcomeGates", g)
		}
	}
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
// another name. The legacy Outcome in gate.go is the one exception, and it
// leaves with the last legacy gate.
func TestNoAdapterDeclaresItsOwnVerdict(t *testing.T) {
	for _, af := range parseAdapters(t) {
		for _, d := range af.file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Name.Name != "Verdict" {
				continue
			}
			if af.path == "gate.go" && recvName(fd) == "Outcome" {
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
