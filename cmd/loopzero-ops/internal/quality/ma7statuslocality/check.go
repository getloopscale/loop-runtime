// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package ma7statuslocality enforces MA-7 Gates B + C of the M9 db-split
// (ADR .claude/knowledge-base/decisions/2026-07-03-m9-db-store-subpackage-topology.md
// D3; MEET-2026-07-03-001). After 5b relocated the build/release status adapters
// into api/internal/db/store/, the status-mutation machinery MUST live there and
// nowhere else:
//
//   - Gate B: the sqlc-generated status/transition UPDATE funcs
//     (Update{Build,Release}Status*, *IfCurrent) are referenced ONLY from
//     api/internal/db/store/ (and api/internal/db/generated/ where they are
//     defined). An outer-layer or root-db caller invoking them directly would
//     bypass the store's SafeTransition + advisory-lock scope.
//   - Gate C: <machine>.SafeTransition( is called ONLY from
//     api/internal/db/store/. Encoding a transition anywhere else re-introduces
//     the release.Status.State="active" anti-pattern MA-7 forbids.
//
// AST-based (not grep): a comment mentioning buildMachine.SafeTransition (e.g.
// the BuildStore interface doc) is NOT a CallExpr and is correctly ignored; the
// project_wrapper read-cast statemachine.State(...) is a conversion, not
// SafeTransition, and does not match.
package ma7statuslocality

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// Config controls the gate.
type Config struct {
	RepoRoot string
}

// Violation is a forbidden status-machinery reference outside db/store/.
type Violation struct {
	File string
	Line int
	Gate string // "B" (generated status func) or "C" (SafeTransition)
	Name string // the offending call name
}

// Outcome is what the gate returns.
type Outcome struct {
	Pass       bool
	Violations []Violation
	Scanned    int
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// generatedStatusFuncs are the sqlc-generated status/transition UPDATE funcs.
// Referencing any of them outside db/store/ (or db/generated/) is a Gate B
// breach. Extend this set if a new status UPDATE query is added.
var generatedStatusFuncs = map[string]bool{
	"UpdateBuildStatusToRunning":           true,
	"UpdateBuildStatusToTerminal":          true,
	"UpdateBuildStatusOther":               true,
	"UpdateBuildStatusToRunningIfCurrent":  true,
	"UpdateBuildStatusToTerminalIfCurrent": true,
	"UpdateReleaseStatus":                  true,
}

const (
	// scanRoot is the module subtree scanned. Relative to RepoRoot.
	scanRoot = "api"
	// allowedDir is the ONLY dir permitted to reference the status machinery.
	allowedDir = "api/internal/db/store"
	// generatedDir is where the funcs are defined (definitions are not calls).
	generatedDir = "api/internal/db/generated"
)

// Check is the gate entry point.
func Check(cfg Config) (Outcome, error) {
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	root := filepath.Join(cfg.RepoRoot, scanRoot)
	allowed := filepath.Join(cfg.RepoRoot, allowedDir)
	generated := filepath.Join(cfg.RepoRoot, generatedDir)

	if _, err := os.Stat(root); err != nil {
		// api/ absent (partial checkout) — trivially passes.
		return Outcome{Pass: true}, nil
	}

	var all []Violation
	scanned := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".go") {
			return nil
		}
		// db/store is the sanctioned home; db/generated holds the definitions.
		if strings.HasPrefix(path, allowed) || strings.HasPrefix(path, generated) {
			return nil
		}
		scanned++
		v, err := scanFile(path)
		if err != nil {
			return err
		}
		all = append(all, v...)
		return nil
	})
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Pass: len(all) == 0, Violations: all, Scanned: scanned}, nil
}

// ActionableFix returns the operator-facing message for FAIL output.
func ActionableFix() string {
	return "The status-mutation machinery (sqlc Update*Status* funcs / " +
		"<machine>.SafeTransition) must be called ONLY from api/internal/db/store/ " +
		"(MA-7 Gates B/C). Route the mutation through the entity's Store adapter " +
		"(UpdateStatus / UpdateStatusIfCurrent) instead of invoking the generated " +
		"func or SafeTransition directly outside db/store/."
}

// scanFile parses a .go file and flags forbidden CallExprs.
func scanFile(path string) ([]Violation, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	var violations []Violation
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil {
			return true
		}
		name := sel.Sel.Name
		pos := fset.Position(sel.Sel.Pos())
		if name == "SafeTransition" {
			violations = append(violations, Violation{File: path, Line: pos.Line, Gate: "C", Name: name})
			return true
		}
		if generatedStatusFuncs[name] {
			violations = append(violations, Violation{File: path, Line: pos.Line, Gate: "B", Name: name})
		}
		return true
	})
	return violations, nil
}
