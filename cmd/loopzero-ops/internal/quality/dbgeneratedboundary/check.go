// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package dbgeneratedboundary enforces MA-7 Gate A of the M9 db-split
// (docs/meetings/records/MEET-2026-07-03-001.md Decision D3; ADR
// .claude/knowledge-base/decisions/2026-07-03-m9-db-store-subpackage-topology.md).
//
// Invariant (Gate A): NO file under api/internal/{services,routes,server}
// imports the sqlc-generated package api/internal/db/generated.
//
// Why this gate exists:
//
//	The status-mutation surface (release/build SafeTransition + the
//	*Status*/*Transition* UPDATE funcs) lives in the generated package and is
//	wrapped by the db-store adapters. If an outer layer (a service, a route
//	handler, or the `api/cmd` composition root that wires the stores) imported
//	generated directly, it could bypass the store's read-modify-write +
//	advisory-lock scope and mutate status out of band — the exact MA-7 hazard
//	the db-split must never widen. The split enforces MA-7 by grep/AST gates,
//	not by a type bifurcation, so this gate is the load-bearing enforcement of
//	the direct-import half of the boundary.
//
// Scope note: this gate is a DIRECT-import fence. It is complemented by 5b's
// Gate B (the generated *Status*/*Transition* UPDATE funcs referenced only from
// db/store/) and Gate C (SafeTransition called only from db/store/), which
// close the indirect-re-export vector at the symbol level. Gate A alone is NOT
// full MA-7 enforcement.
//
// Baseline: MA-7 was CLEAN before M9 (zero services/routes/server import
// generated); this gate freezes that boundary.
package dbgeneratedboundary

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// Config controls the gate.
type Config struct {
	// RepoRoot is the repository root. Default ".".
	RepoRoot string
}

// Violation captures a forbidden import of the generated package from an
// outer-layer file.
type Violation struct {
	File   string
	Line   int
	Import string
}

// Outcome is what the gate returns.
type Outcome struct {
	Pass        bool
	Violations  []Violation
	Scanned     int      // count of .go files scanned
	MissingDirs []string // forbidden dirs absent from the tree (a rename can
	// silently drop a fence — the dispatch surfaces these as a WARN so the
	// coverage loss is never invisible).
}

const generatedImportPath = "github.com/getloopscale/loopzero/api/internal/db/generated"

// forbiddenDirs are the outer layers that must NOT import the generated
// package. Relative to RepoRoot. `api/cmd` is the composition root that wires
// the stores — the single highest-leverage place a bypass would be authored,
// so it is fenced alongside the request-handling layers.
var forbiddenDirs = []string{
	"api/internal/services",
	"api/internal/routes",
	"api/internal/server",
	"api/cmd",
}

// Check is the gate entry point.
func Check(cfg Config) (Outcome, error) {
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}

	var all []Violation
	var missing []string
	scanned := 0

	for _, sub := range forbiddenDirs {
		dir := filepath.Join(cfg.RepoRoot, sub)
		if _, err := os.Stat(dir); err != nil {
			// Absent dir is tolerated (partial checkouts / early phases) but
			// recorded — a rename that silently drops a fence must not be
			// invisible.
			missing = append(missing, sub)
			continue
		}
		v, n, err := walkDir(dir)
		if err != nil {
			return Outcome{}, err
		}
		all = append(all, v...)
		scanned += n
	}

	return Outcome{Pass: len(all) == 0, Violations: all, Scanned: scanned, MissingDirs: missing}, nil
}

// ActionableFix returns the operator-facing message for FAIL output.
func ActionableFix() string {
	return "An outer layer (services/routes/server/cmd) imports " +
		generatedImportPath + " directly, bypassing the db-store adapter's " +
		"read-modify-write + advisory-lock scope (MA-7 Gate A). Route the access " +
		"through the entity's Store adapter in api/internal/db/store/ instead of " +
		"importing the generated package."
}

// walkDir walks dir for .go files (test files INCLUDED — a test that imports
// generated from an outer layer is still a boundary breach) and collects
// forbidden imports.
func walkDir(dir string) ([]Violation, int, error) {
	var violations []Violation
	count := 0

	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".go") {
			return nil
		}
		count++
		v, err := scanFile(path)
		if err != nil {
			return err
		}
		violations = append(violations, v...)
		return nil
	})

	return violations, count, err
}

// scanFile parses a single .go file and returns a violation per forbidden
// import found. Import-only parse (no function bodies) keeps it fast.
func scanFile(path string) ([]Violation, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	var violations []Violation
	for _, imp := range file.Imports {
		if imp.Path == nil {
			continue
		}
		if strings.Trim(imp.Path.Value, `"`) == generatedImportPath {
			pos := fset.Position(imp.Pos())
			violations = append(violations, Violation{
				File:   path,
				Line:   pos.Line,
				Import: generatedImportPath,
			})
		}
	}
	return violations, nil
}
