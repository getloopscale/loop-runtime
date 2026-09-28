// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testskipabsentpath

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/skipshape"
)

// scan walks the tree one PACKAGE DIRECTORY at a time. Per-directory rather
// than per-file because a test's path helper often lives in a non-test file of
// the same package (chartrender's repoRoot does), and a per-file resolver would
// report those guards unresolvable for a reason that is about this gate rather
// than about the tree.
func scan(out *Outcome, root, scanRoot string) error {
	var dirs []string
	err := filepath.WalkDir(scanRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		switch d.Name() {
		case ".git", "node_modules", "vendor", "testdata", ".cache":
			return fs.SkipDir
		}
		dirs = append(dirs, path)
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		if err := scanDir(out, root, dir); err != nil {
			return err
		}
	}
	sort.Slice(out.Sites, func(i, j int) bool {
		if out.Sites[i].File != out.Sites[j].File {
			return out.Sites[i].File < out.Sites[j].File
		}
		return out.Sites[i].Line < out.Sites[j].Line
	})
	return nil
}

// parsedFile carries a file's AST plus the repo-relative path it was read
// from, which the runtime.Caller idiom resolves to.
type parsedFile struct {
	rel  string
	fset *token.FileSet
	ast  *ast.File
}

// scanDir analyses one package directory: it parses every Go file in it, builds
// the package-wide index, and reads each _test.go file for guarded skips.
func scanDir(out *Outcome, root, dir string) error {
	files, err := parsePackageDir(root, dir)
	if err != nil || len(files) == 0 {
		return err
	}
	pkg := indexPackage(files)
	pkgDir := filepath.ToSlash(mustRel(root, dir))
	for _, pf := range files {
		if !strings.HasSuffix(pf.rel, "_test.go") {
			continue
		}
		out.FilesScanned++
		analyseFile(out, root, pkgDir, pkg, pf)
	}
	return nil
}

// parsePackageDir parses every .go file directly in dir — test and non-test
// alike, because a test's path helper often lives in a non-test file of the same
// package. An unparseable file is an error: it is precisely the file whose guards
// would go unread.
func parsePackageDir(root, dir string) ([]parsedFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []parsedFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		rel, rerr := filepath.Rel(root, full)
		if rerr != nil {
			rel = full
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, full, nil, parser.SkipObjectResolution)
		if perr != nil {
			return nil, fmt.Errorf("parsing %s: %w", filepath.ToSlash(rel), perr)
		}
		files = append(files, parsedFile{rel: filepath.ToSlash(rel), fset: fset, ast: f})
	}
	return files, nil
}

// indexPackage builds the package-wide binding scope from every file of one
// directory: zero-argument path helpers, every function and method by name, and
// file-level const/var literals.
func indexPackage(files []parsedFile) *pkgScope {
	pkg := &pkgScope{
		helpers:    map[string]helperDecl{},
		funcs:      map[string]helperDecl{},
		methods:    map[string][]helperDecl{},
		imports:    map[string]map[string]bool{},
		consts:     map[string]boundExpr{},
		funcTypes:  map[string]bool{},
		funcFields: map[string]bool{},
		boundFuncs: map[string][]boundExpr{},
		rebound:    map[string]bool{},
	}
	// Named func types first, across every file: a field spelled `probe
	// predicate` cannot be recognised as func-typed until `type predicate
	// func() bool` has been read, and the two may sit in different files.
	for _, pf := range files {
		indexFuncTypes(pkg, pf)
	}
	for _, pf := range files {
		pkg.imports[pf.rel] = importNames(pf.ast)
		indexFuncFields(pkg, pf)
		indexRebindings(pkg, pf)
		for _, decl := range pf.ast.Decls {
			indexDecl(pkg, pf.rel, decl)
		}
	}
	return pkg
}

// indexDecl records one top-level declaration: a function by name (and as a
// helper when it takes no argument), a method under its name, or the literal
// value of each const/var name that has one.
func indexDecl(pkg *pkgScope, file string, decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		h := helperDecl{decl: d, file: file}
		if d.Recv != nil {
			pkg.methods[d.Name.Name] = append(pkg.methods[d.Name.Name], h)
			return
		}
		pkg.funcs[d.Name.Name] = h
		if d.Type.Params.NumFields() == 0 {
			pkg.helpers[d.Name.Name] = h
		}
	case *ast.GenDecl:
		if d.Tok != token.CONST && d.Tok != token.VAR {
			return
		}
		for _, spec := range d.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != len(vs.Values) {
				continue
			}
			for i, name := range vs.Names {
				pkg.consts[name.Name] = boundExpr{expr: vs.Values[i], file: file}
			}
		}
	}
}

// importNames is the set of names a file's imports bind, so `pkg.Present()` is
// never read as a method of this package. A dot-import binds no name this
// resolver can see, so it is recorded under "." and never matches a receiver.
func importNames(f *ast.File) map[string]bool {
	names := map[string]bool{}
	for _, spec := range f.Imports {
		if spec.Name != nil {
			names[spec.Name.Name] = true
			continue
		}
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		names[path[strings.LastIndex(path, "/")+1:]] = true
	}
	return names
}

func mustRel(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return dir
	}
	return rel
}

// analyseFile counts skip calls (the floor) and records path-guarded ones.
func analyseFile(out *Outcome, root, pkgDir string, pkg *pkgScope, pf parsedFile) {
	ast.Inspect(pf.ast, func(n ast.Node) bool {
		if skipshape.IsSkipCall(n) {
			out.SkipCalls++
		}
		return true
	})

	for _, decl := range pf.ast.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		r := &resolver{root: root, pkgDir: pkgDir, file: pf.rel, pkg: pkg}
		for _, sp := range guardedSkips(fd, r, map[string]bool{fd.Name.Name: true}, 0) {
			s := sp.Site
			s.File = pf.rel
			s.Func = fd.Name.Name
			s.Line = pf.fset.Position(sp.pos).Line
			out.Sites = append(out.Sites, s)
		}
	}
}
