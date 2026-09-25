// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package correlationheader implements the correlation-header quality
// gate (T6.1 of plan 2026-06-01-p3-infra-observability-test-debt v1.1,
// ADR D5 — closes finding val-002).
//
// The gate scans every HTTP package of api/internal for direct
// `r.Header.Get("X-Correlation-ID")` (or `"Correlation-ID"`) literal
// usage and flags each as a violation. Routes MUST go through
// `api/internal/correlation.Read(r)` instead, which:
//  1. Returns the canonical `Correlation-ID` header.
//  2. Falls back to the deprecated `X-Correlation-ID` for one release
//     cycle with a slog WARN.
//  3. Is the single authoritative source for the dual-read logic.
//
// Centralizing the header read into one helper guarantees that:
//   - Distributed tracing across promote/rollback/build flows uses
//     a uniform header form (eliminates val-002).
//   - The deprecation window for `X-Correlation-ID` is observable via
//     the WARN logs.
//   - Future migration to drop the legacy header touches ONE file.
//
// Scope:
//   - every `<feature>/<name>http` package (recursive) — the handlers are
//     the correlation-ID-reading surface.
//   - `*_test.go` files are excluded (fixtures may reference the
//     literals for negative-path coverage).
package correlationheader

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/featurefence"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/scanresult"
)

// Config tells Check which repo to scan.
type Config struct {
	RepoRoot string
}

// scanDirs are the handler trees: every HTTP package of api/internal. The gate
// is intentionally narrow: middleware/, the feature services, db/, etc.
// operate on the in-context correlation ID; only HTTP handlers read headers off
// the *http.Request.
func scanDirs(repoRoot string) ([]string, error) {
	dirs, err := featurefence.HTTPPackageDirs(repoRoot)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil // no tree at all: Check reports that as nothing scanned
	}
	if err != nil {
		return nil, fmt.Errorf("correlation-header: %w", err)
	}
	return dirs, nil
}

// targetMethod is the method we look for: r.Header.Get("...").
// We match by method name + arg-count rather than receiver-type
// (avoiding full type resolution — go/types — which is overkill for
// this narrow gate). Two false-positive guards:
//   - arg must be a basic-lit string (skips var-named args).
//   - literal value must match one of the gated forms.
const targetMethod = "Get"

// gatedLiterals are the EXACT string args that trigger a violation
// when passed to r.Header.Get. Both forms (canonical + legacy) are
// gated because BOTH should funnel through correlation.Read for
// uniform observability + deprecation-warn emission.
var gatedLiterals = map[string]bool{
	`"Correlation-ID"`:   true,
	`"X-Correlation-ID"`: true,
}

// Check walks the handler trees and reports every source file that contains
// r.Header.Get("(X-)?Correlation-ID"). No handler tree, or trees holding no
// production file, is scanresult.ErrNothingScanned: the gate cannot pass over
// code it never read.
func Check(cfg Config) (scanresult.Result, error) {
	var res scanresult.Result
	dirs, err := scanDirs(cfg.RepoRoot)
	if err != nil {
		return res, err
	}
	if len(dirs) == 0 {
		return res, fmt.Errorf("%w: no HTTP package under api/internal", scanresult.ErrNothingScanned)
	}
	for _, d := range dirs {
		if err := scanTree(cfg.RepoRoot, d, &res); err != nil {
			return res, err
		}
	}
	return res, res.Err()
}

func scanTree(repoRoot, dir string, res *scanresult.Result) error {
	return filepath.Walk(filepath.Join(repoRoot, dir), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		hits, err := scanFile(path, rel)
		if err != nil {
			return err
		}
		res.Scanned++
		res.Findings = append(res.Findings, hits...)
		return nil
	})
}

// scanFile parses the .go file via go/parser and walks the AST for
// any CallExpr matching the pattern `<X>.Header.Get(<literal>)` where
// the literal is in gatedLiterals. Pure structural match — no type
// resolution required — but precise enough to avoid:
//   - Matching `.Get(...)` on non-Header receivers (e.g. context.Get).
//   - Matching variable-named args (e.g. `r.Header.Get(headerName)`).
//   - Matching comment-strings (go/parser ignores comments).
func scanFile(absPath, relPath string) ([]string, error) {
	// #nosec G304 -- absPath comes from filepath.Walk of a handler tree.
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
