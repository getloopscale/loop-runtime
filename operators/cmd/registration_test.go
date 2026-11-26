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

// binaryWiringEntry describes a single (binary, package) pair to verify.
//
// ADR-0023 D1: split binaries each wire their own controllers in main().
// Unlike the deprecated monolith (cmd/main.go) which used a setupControllers
// helper, split binaries call <pkg>.Setup* directly from main() — the AST
// walker accepts either pattern.
type binaryWiringEntry struct {
	name      string // human-readable binary name (used in t.Run names)
	pkgImport string // last segment of package import (e.g., "tenant", "app")
	pkgDir    string // path relative to operators/cmd/ where Setup funcs live
	mainPath  string // path to the binary's main.go (relative to operators/cmd/)
	setupFunc string // function name that wires controllers ("main" for split, "setupControllers" for monolith)
}

// binaryWiringMatrix enumerates every operator binary that must satisfy the
// "every exported Setup*Controller wired" invariant.
//
// Originally this test ran only against the monolith (cmd/main.go +
// internal/controller/). T1.3 amplified it to all 4 split binaries per
// ADR-0023 D1; T1.4 then removed the monolith entry alongside the monolith
// binary deletion (LOCAL only — production retains the monolith Helm chart
// until Stage D 30d gate per ADR-0023).
var binaryWiringMatrix = []binaryWiringEntry{
	{
		name:      "tenant-operator",
		pkgImport: "tenant",
		pkgDir:    "../internal/tenant",
		mainPath:  "tenant-operator/main.go",
		setupFunc: "setupControllers",
	},
	{
		name:      "app-operator",
		pkgImport: "app",
		pkgDir:    "../internal/app",
		mainPath:  "app-operator/main.go",
		setupFunc: "main",
	},
	{
		name:      "preview-operator",
		pkgImport: "preview",
		pkgDir:    "../internal/preview",
		mainPath:  "preview-operator/main.go",
		setupFunc: "main",
	},
	{
		name:      "platform-operator",
		pkgImport: "platform",
		pkgDir:    "../internal/platform",
		mainPath:  "platform-operator/main.go",
		setupFunc: "main",
	},
}

// TestSetupControllersWiresAllControllers enforces D5(a) from the operators
// remediation plan: every exported Setup*Controller function in the binary's
// internal package MUST be referenced by the binary's wiring entrypoint.
//
// Matrix-driven (T1.3): runs as 5 sub-tests covering monolith + 4 split
// binaries. Adding a new exported Setup* without wiring fails the relevant
// sub-test.
//
// Plan reference: docs/plans/operators-sota-faang-grade-plan.md T1.3.
// ADR reference: docs/meetings/adr/0023-split-tree-canonical.md.
func TestSetupControllersWiresAllControllers(t *testing.T) {
	for _, bin := range binaryWiringMatrix {
		// Go 1.22+: per-iteration loop variable; no shadow needed.
		t.Run(bin.name, func(t *testing.T) {
			exported := exportedSetupControllerFuncs(t, bin.pkgDir)
			wired := wiredSetupControllerCalls(t, bin.mainPath, bin.pkgImport, bin.setupFunc)

			for _, fn := range exported {
				if !contains(wired, fn) {
					t.Errorf("%s/ exports %s() but %s does not call it. "+
						"This is the 'Implemented but not wired' pattern (COMP-001 family). "+
						"Either wire the controller in %s or rename it to be unexported.",
						bin.pkgDir, fn, bin.mainPath, bin.setupFunc)
				}
			}
		})
	}
}

// TestSetupControllersHasNoOrphanCalls catches the inverse pattern: the
// binary's wiring entrypoint references a Setup* that no longer exists in
// its internal package. Protects against rename-without-update bugs.
//
// Matrix-driven (T1.3): runs as 5 sub-tests.
func TestSetupControllersHasNoOrphanCalls(t *testing.T) {
	for _, bin := range binaryWiringMatrix {
		// Go 1.22+: per-iteration loop variable; no shadow needed.
		t.Run(bin.name, func(t *testing.T) {
			exported := exportedSetupControllerFuncs(t, bin.pkgDir)
			wired := wiredSetupControllerCalls(t, bin.mainPath, bin.pkgImport, bin.setupFunc)

			for _, fn := range wired {
				if !contains(exported, fn) {
					t.Errorf("%s calls %s.%s() but no such exported function exists "+
						"in %s/. Likely a rename without %s update.",
						bin.mainPath, bin.pkgImport, fn, bin.pkgDir, bin.mainPath)
				}
			}
		})
	}
}

// SetupAllowlist contains exported Setup* function names that are intentionally
// NOT wired in any binary's main. Each entry MUST have a justification.
// Adding a function here is governance-equivalent to wiring it — a reviewer
// must approve the exception explicitly.
//
// Per ADR-0022 + ADR-0023: this allowlist itself is governed by the
// wired-gate amplification policy.
//
// T1.3: keys may be qualified per binary (e.g., "tenant:SetupCacheReader") to
// allow legitimate exceptions per binary. Bare keys apply to all binaries.
var SetupAllowlist = map[string]string{}

func exportedSetupControllerFuncs(t *testing.T, dir string) []string {
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
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			name := fn.Name.Name
			// AMPLIFIED gate (per ADR-0022 + ADR-0023): match all exported `Setup*`
			// (was `Setup*Controller`). Catches webhooks, reconcilers, cache readers,
			// metrics servers — any setup-pattern that landed exported but not wired.
			// Allowlisted entries are skipped (intentional exceptions).
			if strings.HasPrefix(name, "Setup") && ast.IsExported(name) {
				if _, allowed := SetupAllowlist[name]; allowed {
					continue
				}
				found = append(found, name)
			}
		}
	}
	return found
}

// wiredSetupControllerCalls walks the main.go AST and finds calls of the form
// <pkgImport>.Setup*(...) inside the function named setupFunc.
//
// For monolith (setupFunc="setupControllers"): scans inside the helper
// function. For split binaries (setupFunc="main"): scans the entire main()
// since wiring happens directly there.
func wiredSetupControllerCalls(t *testing.T, mainPath, pkgImport, setupFunc string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, mainPath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", mainPath, err)
	}
	var found []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != setupFunc {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok || ident.Name != pkgImport {
				return true
			}
			name := sel.Sel.Name
			// Match same pattern as exportedSetupControllerFuncs:
			// Setup* prefix only, no suffix constraint.
			if strings.HasPrefix(name, "Setup") {
				found = append(found, name)
			}
			return true
		})
	}
	return found
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
