// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T3.6 of plan 2026-06-11-cleanroom-canonical-5-defense-layers v1.2.
//
// Hermetic Go AST gate asserting every public HTTP handler method
// defined in cloud/internal/routes/ has at least 1 corresponding
// route registration in cloud/cmd/main.go. Closes catch #19 from T6.2
// session (memory reference_t62_cleanroom_session_2026_06_11 +
// j05-aprendizado-orphan-handler-accept-route-not-wired): the Accept
// handler for /device/accept was implemented and tested, but cloud/cmd/main.go
// was missing the r.Post("/device/accept", handler.Accept) line —
// orphan handler. Compile clean + unit tests GREEN, but route returned 404.
//
// Pattern: walk routes/*.go with go/ast extracting method names on
// Handler structs. Walk cmd/main.go for chi/Post/Get/Put/Delete/Patch/Handle
// references targeting those methods. Each orphan = test failure.
//
// Currently this test is a SCAFFOLD asserting structural readiness for
// the canonical gate. Full AST handler-extraction + route-cross-reference
// is the GREEN target (referenced by T1.4/T1.5 of the plan).

package unit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRouteRegistrationCanonical walks cloud/cmd/main.go and asserts that
// the known critical handlers (per T6.2 session catches) are registered.
// This is the REGRESSION DEFENSE for catch #19 specifically — Accept handler.
func TestRouteRegistrationCanonical(t *testing.T) {
	// Locate cmd/main.go relative to this test file.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// cwd is cloud/tests/unit — walk up 2 levels to cloud/, then cmd/main.go
	mainPath := filepath.Join(cwd, "..", "..", "cmd", "main.go")
	if _, err := os.Stat(mainPath); os.IsNotExist(err) {
		t.Skipf("cmd/main.go not found at %s — skipping canonical route gate", mainPath)
		return
	}

	// Parse main.go via go/ast.
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, mainPath, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}

	// Collect all string literals + selector identifiers (handler.Method references).
	// We look for tuples (path, handlerCall) inside r.Post/r.Get/r.Put/r.Delete/r.Patch.
	registeredHandlers := make(map[string]bool)

	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		method := sel.Sel.Name
		// We care about HTTP-verb chi methods.
		switch method {
		case "Post", "Get", "Put", "Delete", "Patch", "Handle", "HandleFunc":
			// Each call has args; typically (path, handlerRef). Extract handlerRef.
			for _, arg := range call.Args {
				if selExpr, ok := arg.(*ast.SelectorExpr); ok {
					if x, ok := selExpr.X.(*ast.Ident); ok {
						handlerKey := x.Name + "." + selExpr.Sel.Name
						registeredHandlers[handlerKey] = true
					}
				}
			}
		}
		return true
	})

	// === Critical handler regression-defense list ===
	// These handlers MUST be registered. Each cites the catch that motivated the assertion.
	criticalHandlers := []struct {
		name  string
		catch string
	}{
		{"deviceHandler.Accept", "catch #19 — J-05 device-flow accept orphan"},
		{"deviceHandler.Authorize", "J-05 device-flow base"},
		{"deviceHandler.Token", "J-05 device-flow token exchange"},
	}

	missing := []string{}
	for _, ch := range criticalHandlers {
		if !registeredHandlers[ch.name] {
			missing = append(missing, ch.name+" (" + ch.catch + ")")
		}
	}

	if len(missing) > 0 {
		t.Errorf("orphan handlers (implemented but no route registration):\n  - %s", strings.Join(missing, "\n  - "))
	}

	// Coverage signal: at least 5 handler registrations found (sanity check the AST walk).
	if len(registeredHandlers) < 5 {
		t.Errorf("AST walk found only %d route registrations — main.go may have changed structure or AST parser missed selectors. Inspect manually.", len(registeredHandlers))
	}

	t.Logf("AST gate: %d total route registrations found, %d critical handlers verified", len(registeredHandlers), len(criticalHandlers)-len(missing))
}
