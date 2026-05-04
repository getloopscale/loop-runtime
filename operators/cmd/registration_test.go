// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetupControllersWiresAllControllers enforces D5(a) from the operators
// remediation plan: every exported Setup*Controller function in
// internal/controller/ MUST be referenced by setupControllers in main.go.
//
// This is the systemic fix for the recurring "Implemented but not wired"
// pattern (COMP-001 PreviewController, ARCH-001 MinIO CB wrapper, COMP-008
// PR webhook handler).
//
// CURRENT STATUS (2026-04-26 — post T2.1): unskipped because
// SetupPreviewController is now wired in setupControllers.
// This test fails any future PR that adds an exported Setup*Controller
// function without registering it in setupControllers.
//
// Plan reference: docs/plans/operators-deep-review-remediation-plan.md T3.8.
// ADR reference: .claude/decisions/2026-04-26-operators-remediation-classification.md.
func TestSetupControllersWiresAllControllers(t *testing.T) {
	exported := exportedSetupControllerFuncs(t, "../internal/controller")
	wired := wiredSetupControllerCalls(t, "main.go")

	for _, fn := range exported {
		if !contains(wired, fn) {
			t.Errorf("internal/controller exports %s() but main.go:setupControllers does not call it. "+
				"This is the 'Implemented but not wired' pattern (COMP-001 family). "+
				"Either wire the controller in setupControllers or rename it to be unexported.", fn)
		}
	}
}

// TestSetupControllersHasNoOrphanCalls catches the inverse pattern: main.go
// references a Setup*Controller that no longer exists in internal/controller.
// This protects against rename-without-update bugs.
//
// Always-on (no skip) — this direction must always hold.
func TestSetupControllersHasNoOrphanCalls(t *testing.T) {
	exported := exportedSetupControllerFuncs(t, "../internal/controller")
	wired := wiredSetupControllerCalls(t, "main.go")

	for _, fn := range wired {
		if !contains(exported, fn) {
			t.Errorf("main.go:setupControllers calls controller.%s() but no such exported function exists "+
				"in internal/controller/. Likely a rename without main.go update.", fn)
		}
	}
}

// SetupAllowlist contains exported Setup* function names that are intentionally
// NOT wired in main.go's setupControllers. Each entry MUST have a justification.
// Adding a function here is governance-equivalent to wiring it — a reviewer
// must approve the exception explicitly.
//
// Per ADR-0022: this allowlist itself is governed by the wired-gate
// amplification policy (Plan T0.2 of `pre-launch-gaps-plan.md`).
var SetupAllowlist = map[string]string{
	// (none yet — populate as legitimate exceptions arise)
	// Example template:
	// "SetupCacheReader": "test-only helper, not part of controller-runtime wiring",
}

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
			// AMPLIFIED gate (per ADR-0022 + Plan T0.2): match all exported `Setup*`
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

func wiredSetupControllerCalls(t *testing.T, mainPath string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, mainPath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", mainPath, err)
	}
	var found []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "setupControllers" {
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
			if !ok || ident.Name != "controller" {
				return true
			}
			name := sel.Sel.Name
			// Match same pattern as exportedSetupControllerFuncs (Plan T0.2 + ADR-0022):
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
