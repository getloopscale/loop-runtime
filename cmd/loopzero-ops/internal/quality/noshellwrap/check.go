// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package noshellwrap implements the D5 governance gate from
// 2026-05-22-shell-script-strategic-plan (translated): "Shell-wrap in Go is
// FORBIDDEN — Go code that shells out to a CLI when a native Go SDK exists is
// technical debt."
//
// Detection (AST-based, conservative):
//
//  1. Walk root directory; skip vendor/, node_modules/, .git/, _legacy/
//  2. Parse every non-test Go file
//  3. Find call expressions matching `<obj>.Run("<literal>", ...)` where
//     - the selector is `Run` (heuristic: shellrun.Runner.Run)
//     - the first argument is a string literal
//  4. If literal is in ForbiddenCLI map → check for //nolint:shell-wrap
//     in the source line OR the immediately preceding line
//  5. If no nolint suppression → violation
//
// LIMITATIONS (documented; acceptable per plan Anti-Pattern #8 note):
//   - Dynamic CLIs (variable instead of literal) NOT detected — code review backstop
//   - Non-Runner types with Run() method NOT distinguished — heuristic accepts
//     occasional false positive in exchange for KISS implementation
package noshellwrap

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

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// ForbiddenCLI lists CLIs that have mature Go SDK alternatives. Shell-wrap of
// these in production code (pkg/, cmd/) violates D5 unless suppressed with
// //nolint:shell-wrap + a justification.
var ForbiddenCLI = map[string]bool{
	"pulumi":  true, // use github.com/pulumi/pulumi/sdk/v3 Automation API
	"helm":    true, // use helm.sh/helm/v3 SDK
	"kubectl": true, // use k8s.io/client-go
	"aws":     true, // use github.com/aws/aws-sdk-go-v2
	"gh":      true, // use github.com/google/go-github
}

// AllowedCLI lists CLIs where shellrun is legitimate (no mature Go SDK or
// stdlib equivalent suffices). Documented here for operator clarity;
// gate just refuses to flag these.
var AllowedCLI = map[string]bool{
	"git":    true, // no good Go alternative; go-git incomplete
	"diff":   true, // system util
	"sed":    true, // system util
	"awk":    true, // system util
	"grep":   true, // system util (regexp stdlib often suffices but not always)
	"docker": true, // Go SDKs exist but immature (moby/moby)
	"k3d":    true, // no Go SDK
}

// Config tells Check where to operate.
type Config struct {
	Root string // absolute or repo-relative directory to scan
}

// Outcome captures Check result.
type Outcome struct {
	Pass       bool
	Violations []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// excludedDirs — paths skipped during walk.
var excludedDirs = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	".git":         true,
	"_legacy":      true,
	"testdata":     true,
}

// ErrNothingScanned means the root held no non-test Go file.
var ErrNothingScanned = errors.New("no non-test Go file under the root — the gate read nothing")

// Check is the entry point invoked by `loopzero-ops quality no-shell-wrap`.
func Check(cfg Config) (*Outcome, error) {
	outcome := &Outcome{Pass: true}
	scanned := 0

	err := filepath.WalkDir(cfg.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if excludedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		if strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		scanned++
		viols, scanErr := scanFile(path)
		if scanErr != nil {
			// An unparseable file is the one whose shell-wraps would go unread.
			return scanErr
		}
		if len(viols) > 0 {
			outcome.Pass = false
			outcome.Violations = append(outcome.Violations, viols...)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", cfg.Root, err)
	}
	if scanned == 0 {
		return nil, fmt.Errorf("%s: %w", cfg.Root, ErrNothingScanned)
	}
	return outcome, nil
}

// scanFile parses a single Go file and returns violation messages.
func scanFile(path string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	// Read raw source for comment-based suppression detection.
	srcBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	srcLines := strings.Split(string(srcBytes), "\n")

	var violations []string

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		// Match `<x>.Run` selector pattern
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name != "Run" {
			return true
		}

		// First argument must be a basic string literal
		if len(call.Args) == 0 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok {
			return true
		}
		if lit.Kind != token.STRING {
			return true
		}

		// Strip Go string quoting
		cli := strings.Trim(lit.Value, "\"`")

		// Only flag forbidden CLIs; AllowedCLI is implicit pass
		if !ForbiddenCLI[cli] {
			return true
		}

		// Resolve position
		pos := fset.Position(call.Pos())
		lineIdx := pos.Line - 1 // 0-indexed for slice

		// Check for nolint suppression: same line OR previous line
		if hasNolintSuppression(srcLines, lineIdx) {
			return true
		}

		violations = append(violations, fmt.Sprintf(
			"%s:%d: shellrun.Run(%q, ...) wraps forbidden CLI %q; use Go SDK alternative OR add //nolint:shell-wrap + justification per plan D5",
			path, pos.Line, cli, cli))

		return true
	})

	return violations, nil
}

// hasNolintSuppression returns true if the call line OR the line immediately
// above contains a `//nolint:shell-wrap` directive.
func hasNolintSuppression(srcLines []string, lineIdx int) bool {
	const marker = "//nolint:shell-wrap"
	check := func(idx int) bool {
		if idx < 0 || idx >= len(srcLines) {
			return false
		}
		return strings.Contains(srcLines[idx], marker)
	}
	return check(lineIdx) || check(lineIdx-1)
}
