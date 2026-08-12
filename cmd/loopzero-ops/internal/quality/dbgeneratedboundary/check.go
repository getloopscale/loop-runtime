// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package dbgeneratedboundary enforces MA-7 Gate A of the M9 db-split
// (docs/meetings/records/MEET-2026-07-03-001.md Decision D3; ADR
// .claude/knowledge-base/decisions/2026-07-03-m9-db-store-subpackage-topology.md).
//
// Invariant (Gate A): NO file under api/internal/{services,routes,server}
// imports the sqlc-generated package api/internal/db/generated — and, in the
// layers measured free of it, the hand-written api/internal/db as well.
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

// handWrittenDBPath is the hand-written persistence package — db.Build,
// db.Release, db.DBTX — as distinct from the sqlc-generated one above.
const handWrittenDBPath = "github.com/getloopscale/loopzero/api/internal/db"

// dirsForbiddenFromHandWrittenDB is DELIBERATELY narrower than forbiddenDirs.
//
// B-049's DoD asserts that widening the fence from `db/generated` to `db` is
// "literally a string". Measured 2026-08-12, it is not. Under the semantics this
// gate actually uses — test files INCLUDED, see walkDir — `api/internal/services`
// carries 51 importers and `api/cmd` 18: 69 files that would fail the day the
// fence widened (71 before slice-6 took routes to zero). A gate that fails 43 files on arrival is
// not a fence; it is an exemption at birth, which is the debt slice-1 spent
// itself removing.
//
// `api/internal/server` was measured at ZERO, so the fence lands there green and
// permanent: from today the first persistence import into the server layer is
// refused. The other three carry their counts in the backlog rather than an
// exemption nobody can read.
var dirsForbiddenFromHandWrittenDB = []string{
	"api/internal/server",
	// Added by slice-6: routes reached ZERO importers when db.DBTX — the only
	// symbol it used, in two files — moved to the api/internal/db/dbtx leaf.
	// The interface layer no longer names a persistence type at all.
	"api/internal/routes",
}

// Check is the gate entry point.
func Check(cfg Config) (Outcome, error) {
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}

	var all []Violation
	var missing []string
	scanned := 0

	handWritten := map[string]bool{}
	for _, d := range dirsForbiddenFromHandWrittenDB {
		handWritten[d] = true
	}

	for _, sub := range forbiddenDirs {
		dir := filepath.Join(cfg.RepoRoot, sub)
		if _, err := os.Stat(dir); err != nil {
			// Absent dir is tolerated (partial checkouts / early phases) but
			// recorded — a rename that silently drops a fence must not be
			// invisible.
			missing = append(missing, sub)
			continue
		}
		v, n, err := walkDir(dir, handWritten[sub])
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
	return "An outer layer imports a persistence package it must not.\n" +
		"  · " + generatedImportPath + " — forbidden in services/routes/server/cmd. It\n" +
		"    bypasses the db-store adapter's read-modify-write + advisory-lock scope\n" +
		"    (MA-7 Gate A). Route the access through the entity's Store adapter in\n" +
		"    api/internal/db/store/ instead.\n" +
		"  · " + handWrittenDBPath + " — forbidden in " + strings.Join(dirsForbiddenFromHandWrittenDB, ", ") +
		" only.\n    That fence is deliberately narrower than the generated one: the other\n" +
		"    layers — services and cmd — carry 69 importers today, and a gate that fails\n" +
		"    arrival is an exemption at birth. Widening it is B-079."
}

// walkDir walks dir for .go files (test files INCLUDED — a test that imports
// generated from an outer layer is still a boundary breach) and collects
// forbidden imports.
func walkDir(dir string, alsoHandWritten bool) ([]Violation, int, error) {
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
		v, err := scanFile(path, alsoHandWritten)
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
func scanFile(path string, alsoHandWritten bool) ([]Violation, error) {
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
		imported := strings.Trim(imp.Path.Value, `"`)
		// The hand-written package is fenced only in the layers measured free of
		// it — see dirsForbiddenFromHandWrittenDB for why the fence is narrower
		// than the generated one.
		if alsoHandWritten && imported == handWrittenDBPath {
			violations = append(violations, Violation{
				File:   path,
				Line:   fset.Position(imp.Pos()).Line,
				Import: handWrittenDBPath,
			})
			continue
		}
		if imported == generatedImportPath {
			violations = append(violations, Violation{
				File:   path,
				Line:   fset.Position(imp.Pos()).Line,
				Import: generatedImportPath,
			})
		}
	}
	return violations, nil
}
