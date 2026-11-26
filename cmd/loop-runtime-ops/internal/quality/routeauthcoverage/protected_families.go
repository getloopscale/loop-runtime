// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package routeauthcoverage

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// serverPackageDir is the package router.go lives in.
const serverPackageDir = "api/internal/server"

// protectedEntry is the function the Auth group hands its router to.
const protectedEntry = "registerProtectedRoutes"

// funcSpan is one function's line range in one file.
type funcSpan struct {
	file       string
	start, end int
}

// protectedFunctions follows registerProtectedRoutes wherever it lives in the
// server package: its own body and every function it reaches — called directly,
// or listed in a package-level composite literal (a slice of family
// registrars) that its body names. It returns their spans and the routes they
// register.
//
// The route set of the Auth group is exactly this closure: when the family
// registrars moved out of router.go into protected_routes.go, a gate reading
// router.go alone counted 14 routes on a system with ~70 and reported the
// collapse as an instrumentation failure while every family route was still
// behind Auth.
func protectedFunctions(root string) ([]funcSpan, []RouteSite, error) {
	dir := filepath.Join(root, serverPackageDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	fset := token.NewFileSet()
	funcs := map[string]*ast.FuncDecl{}
	funcFile := map[string]string{}
	lists := map[string][]string{} // package-level var → function idents in its composite literal
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(serverPackageDir, name))
		f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if perr != nil {
			continue
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Body != nil {
					funcs[d.Name.Name] = d
					funcFile[d.Name.Name] = rel
				}
			case *ast.GenDecl:
				collectFuncLists(d, lists)
			}
		}
	}
	if _, ok := funcs[protectedEntry]; !ok {
		return nil, nil, nil
	}

	var spans []funcSpan
	var sites []RouteSite
	seen := map[string]bool{}
	queue := []string{protectedEntry}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		seen[name] = true
		fn := funcs[name]
		rel := funcFile[name]
		spans = append(spans, funcSpan{
			file:  rel,
			start: fset.Position(fn.Body.Lbrace).Line,
			end:   fset.Position(fn.Body.Rbrace).Line,
		})
		sites = append(sites, enumerateNode(fset, fn.Body, rel)...)
		for _, ref := range referencedIdents(fn.Body) {
			if _, isFunc := funcs[ref]; isFunc {
				queue = append(queue, ref)
			}
			for _, member := range lists[ref] {
				if _, isFunc := funcs[member]; isFunc {
					queue = append(queue, member)
				}
			}
		}
	}
	return spans, sites, nil
}

// collectFuncLists records, for each package-level var initialised with a
// composite literal, the plain identifiers it lists.
func collectFuncLists(d *ast.GenDecl, lists map[string][]string) {
	if d.Tok != token.VAR {
		return
	}
	for _, spec := range d.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for i, value := range vs.Values {
			lit, ok := value.(*ast.CompositeLit)
			if !ok || i >= len(vs.Names) {
				continue
			}
			for _, elt := range lit.Elts {
				if id, ok := elt.(*ast.Ident); ok {
					lists[vs.Names[i].Name] = append(lists[vs.Names[i].Name], id.Name)
				}
			}
		}
	}
}

// referencedIdents lists every identifier a block names.
func referencedIdents(block *ast.BlockStmt) []string {
	var out []string
	ast.Inspect(block, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			out = append(out, id.Name)
		}
		return true
	})
	return out
}

// calls reports whether block calls the function named name.
func calls(block *ast.BlockStmt, name string) bool {
	found := false
	ast.Inspect(block, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == name {
				found = true
			}
		}
		return !found
	})
	return found
}

// inSpans reports whether file:line falls inside one of spans.
func inSpans(spans []funcSpan, file string, line int) bool {
	for _, s := range spans {
		if s.file == file && line >= s.start && line <= s.end {
			return true
		}
	}
	return false
}
