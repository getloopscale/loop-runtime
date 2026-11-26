// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestMainCallsAllSetupFunctions enforces that every setup* function defined
// in cmd/setup_*.go is called from SOME function in cmd/*.go (main.go or
// other setup files — setup functions can compose each other).
//
// This is the API equivalent of operators/cmd/registration_test.go — it catches
// the recurring "implemented but not wired" pattern (COMP-001 family) where
// a setup function exists with correct implementation but is never called
// from any entrypoint.
//
// If this test fails, either:
//   - Wire the setup function in main() or another setup function
//   - Delete the setup function if it's no longer needed
//   - Move it to a non-setup_*.go file if it's a helper
func TestMainCallsAllSetupFunctions(t *testing.T) {
	defined := setupFunctionsInFiles(t, "setup_*.go")
	called := allFunctionCallsInDir(t, ".")

	for _, fn := range defined {
		if !contains(called, fn) {
			t.Errorf("setup function %s() is defined in cmd/setup_*.go but never called from any cmd/*.go file. "+
				"This is the 'Implemented but not wired' pattern. "+
				"Either call it from main() or remove it.", fn)
		}
	}
}

// allFunctionCallsInDir parses all non-test Go files in the directory and
// returns every function name that appears as a call expression.
func allFunctionCallsInDir(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	fset := token.NewFileSet()
	var found []string
	for _, p := range matches {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				found = append(found, fun.Name)
			case *ast.SelectorExpr:
				found = append(found, fun.Sel.Name)
			}
			return true
		})
	}
	return found
}

// TestMainHasNoOrphanSetupCalls catches the inverse: main.go calls a setup
// function that no longer exists in any setup_*.go file.
// Only checks functions defined in setup_*.go that are referenced from main.go
// (ignores functions defined in main.go itself like setupBuildHandler).
func TestMainHasNoOrphanSetupCalls(t *testing.T) {
	defined := setupFunctionsInFiles(t, "setup_*.go")
	calledFromMain := setupCallsInMainFunc(t, "main.go")

	for _, fn := range calledFromMain {
		// Only flag if it looks like it SHOULD be in setup_*.go
		// (called from main() and starts with "setup") but doesn't exist there.
		// Functions defined in main.go itself (like setupBuildHandler) are fine.
		mainFuncs := functionsDefinedInFile(t, "main.go")
		if contains(mainFuncs, fn) {
			continue // defined in main.go itself, not a setup_*.go concern
		}
		if !contains(defined, fn) {
			t.Errorf("main.go calls %s() but no such function exists in cmd/setup_*.go or main.go. "+
				"Likely a rename without main.go update.", fn)
		}
	}
}

// functionsDefinedInFile returns all top-level function names defined in a file.
func functionsDefinedInFile(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var found []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		found = append(found, fn.Name.Name)
	}
	return found
}

// setupCallsInMainFunc returns setup* function calls made from main() only.
func setupCallsInMainFunc(t *testing.T, mainPath string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, mainPath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", mainPath, err)
	}
	var found []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "main" {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if ident, ok := call.Fun.(*ast.Ident); ok && strings.HasPrefix(ident.Name, "setup") {
				found = append(found, ident.Name)
			}
			return true
		})
	}
	return found
}

// setupFunctionsInFiles parses all files matching the glob pattern and returns
// function names that start with "setup" (exported or unexported).
func setupFunctionsInFiles(t *testing.T, pattern string) []string {
	t.Helper()
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no setup_*.go files found — expected at least one")
	}

	fset := token.NewFileSet()
	var found []string
	for _, p := range matches {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			name := fn.Name.Name
			if strings.HasPrefix(name, "setup") || strings.HasPrefix(name, "Setup") {
				found = append(found, name)
			}
		}
	}
	return found
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
