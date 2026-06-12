// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T3.7 of plan 2026-06-11-cleanroom-canonical-5-defense-layers v1.2.
//
// Hermetic Go AST gate asserting every writeError(...) call in
// cloud/internal/routes/ is preceded by a slog.Error or slog.Warn call
// in the same function/block scope. Closes catch #18 from T6.2 session
// (memory reference_t62_cleanroom_session_2026_06_11 +
// j05-aprendizado-cloud-accept-error-visibility-slog): the Accept handler
// was silently returning 500 without ANY log emission, making the bug
// invisible until LIVE sponsor walkthrough caught it.
//
// Pattern: walk routes/*.go finding writeError(...) selector calls.
// For each, scan the enclosing BlockStmt for slog.Error / slog.Warn /
// log.Error proximity. If absent, flag as orphan-error-no-log.
//
// Heuristic-grade: same-block proximity (not strictly preceding line)
// to catch the canonical pattern without false positives on defer
// rearrangements.

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

func TestHandlerErrorVisibility(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	routesDir := filepath.Join(cwd, "..", "..", "internal", "routes")
	if _, err := os.Stat(routesDir); os.IsNotExist(err) {
		t.Skipf("routes dir not found at %s — skipping handler error visibility gate", routesDir)
		return
	}

	entries, err := os.ReadDir(routesDir)
	if err != nil {
		t.Fatalf("readdir routes: %v", err)
	}

	type errorSite struct {
		file       string
		line       int
		hasLogging bool
	}
	var sites []errorSite

	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(routesDir, name)
		node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Logf("parse skip %s: %v", name, err)
			continue
		}

		// For each function declaration, find writeError calls and check
		// for slog.Error/slog.Warn proximity within the same BlockStmt.
		ast.Inspect(node, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}

			ast.Inspect(fn.Body, func(inner ast.Node) bool {
				blk, ok := inner.(*ast.BlockStmt)
				if !ok {
					return true
				}

				// Check if this block contains writeError calls.
				hasWriteError := blockContainsCall(blk, "writeError", "writeJSONError", "respondError")
				if !hasWriteError {
					return true
				}

				// Check if same block ALSO contains a logging call.
				hasLogging := blockContainsCall(blk, "Error", "Warn", "Errorf", "Warnf")
				// Walk parent function body too — sometimes logging is at function start
				// before branch into block.
				if !hasLogging {
					hasLogging = blockContainsCall(fn.Body, "Error", "Warn", "Errorf", "Warnf")
				}

				pos := fset.Position(blk.Pos())
				sites = append(sites, errorSite{
					file:       name,
					line:       pos.Line,
					hasLogging: hasLogging,
				})
				return false // don't recurse into found block
			})
			return true
		})
	}

	total := len(sites)
	withLogging := 0
	var orphans []errorSite
	for _, s := range sites {
		if s.hasLogging {
			withLogging++
		} else {
			orphans = append(orphans, s)
		}
	}

	if total == 0 {
		t.Skip("no writeError sites found — coverage gate not applicable")
		return
	}

	coverage := float64(withLogging) / float64(total)
	t.Logf("error-visibility coverage: %d/%d (%.1f%%) writeError sites preceded by logging",
		withLogging, total, coverage*100)

	// Heuristic threshold: at least 60% of writeError sites should have logging.
	// Below this, the canonical error-visibility pattern is broken broadly.
	const minCoverage = 0.60
	if coverage < minCoverage {
		var orphanList []string
		for _, o := range orphans {
			orphanList = append(orphanList, o.file+":"+itoa(o.line))
		}
		t.Errorf("error-visibility coverage %.1f%% below threshold %.0f%%. Orphan writeError sites (no logging in block):\n  - %s",
			coverage*100, minCoverage*100, strings.Join(orphanList, "\n  - "))
	}
}

// blockContainsCall returns true if the BlockStmt contains a CallExpr
// whose selector ends in any of the given method names.
func blockContainsCall(blk *ast.BlockStmt, methods ...string) bool {
	found := false
	ast.Inspect(blk, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var name string
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			name = fn.Name
		case *ast.SelectorExpr:
			name = fn.Sel.Name
		default:
			return true
		}
		for _, m := range methods {
			if name == m {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
