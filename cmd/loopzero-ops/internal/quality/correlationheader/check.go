// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package correlationheader implements the correlation-header quality
// gate (T6.1 of plan 2026-06-01-p3-infra-observability-test-debt v1.1,
// ADR D5 — closes finding val-002).
//
// The gate scans `api/internal/routes/*.go` for direct
// `r.Header.Get("X-Correlation-ID")` (or `"Correlation-ID"`) literal
// usage and flags each as a violation. Routes MUST go through
// `api/internal/correlation.Read(r)` instead, which:
//   1. Returns the canonical `Correlation-ID` header.
//   2. Falls back to the deprecated `X-Correlation-ID` for one release
//      cycle with a slog WARN.
//   3. Is the single authoritative source for the dual-read logic.
//
// Centralizing the header read into one helper guarantees that:
//   * Distributed tracing across promote/rollback/build flows uses
//     a uniform header form (eliminates val-002).
//   * The deprecation window for `X-Correlation-ID` is observable via
//     the WARN logs.
//   * Future migration to drop the legacy header touches ONE file.
//
// Scope:
//   * `api/internal/routes/*.go` (recursive) — routes are the
//     correlation-ID-reading surface.
//   * `*_test.go` files are excluded (fixtures may reference the
//     literals for negative-path coverage).
package correlationheader

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// Config tells Check which repo to scan.
type Config struct {
	RepoRoot string
}

// Outcome is the gate result.
type Outcome struct {
	Pass       bool
	Violations []string
}

// scanDir is the production tree scanned for header literals. The gate
// is intentionally narrow: middleware/, services/, db/, etc. operate
// on the in-context correlation ID; only HTTP handlers (routes/) read
// headers off the *http.Request.
const scanDir = "api/internal/routes"

// targetMethod is the method we look for: r.Header.Get("...").
// We match by method name + arg-count rather than receiver-type
// (avoiding full type resolution — go/types — which is overkill for
// this narrow gate). Two false-positive guards:
//   * arg must be a basic-lit string (skips var-named args).
//   * literal value must match one of the gated forms.
const targetMethod = "Get"

// gatedLiterals are the EXACT string args that trigger a violation
// when passed to r.Header.Get. Both forms (canonical + legacy) are
// gated because BOTH should funnel through correlation.Read for
// uniform observability + deprecation-warn emission.
var gatedLiterals = map[string]bool{
	`"Correlation-ID"`:   true,
	`"X-Correlation-ID"`: true,
}

// Check walks the scan directory and returns Outcome.Pass=false if any
// route source file contains r.Header.Get("(X-)?Correlation-ID").
func Check(cfg Config) (*Outcome, error) {
	out := &Outcome{Pass: true}
	root := filepath.Join(cfg.RepoRoot, scanDir)
	if _, err := os.Stat(root); err != nil {
		return out, nil // routes dir absent → nothing to scan
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(cfg.RepoRoot, path)
		hits, scanErr := scanFile(path, rel)
		if scanErr != nil {
			return scanErr
		}
		out.Violations = append(out.Violations, hits...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.Pass = len(out.Violations) == 0
	return out, nil
}

// scanFile parses the .go file via go/parser and walks the AST for
// any CallExpr matching the pattern `<X>.Header.Get(<literal>)` where
// the literal is in gatedLiterals. Pure structural match — no type
// resolution required — but precise enough to avoid:
//   * Matching `.Get(...)` on non-Header receivers (e.g. context.Get).
//   * Matching variable-named args (e.g. `r.Header.Get(headerName)`).
//   * Matching comment-strings (go/parser ignores comments).
func scanFile(absPath, relPath string) ([]string, error) {
	// #nosec G304 -- absPath comes from filepath.Walk of api/internal/routes.
	src, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, absPath, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("correlationheader: parse %s: %w", relPath, err)
	}
	var hits []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		// Match X.Y.Z(...) where Z == "Get".
		outerSel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || outerSel.Sel.Name != targetMethod {
			return true
		}
		// X.Y where Y == "Header".
		innerSel, ok := outerSel.X.(*ast.SelectorExpr)
		if !ok || innerSel.Sel.Name != "Header" {
			return true
		}
		// Must have exactly one arg AND it must be a string lit.
		if len(call.Args) != 1 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if gatedLiterals[lit.Value] {
			pos := fset.Position(call.Pos())
			hits = append(hits, fmt.Sprintf("%s:%d: r.Header.Get(%s) — use api/internal/correlation.Read(r) instead",
				relPath, pos.Line, lit.Value))
		}
		return true
	})
	return hits, nil
}
