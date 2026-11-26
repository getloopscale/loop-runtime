// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package dbgeneratedboundary enforces MA-7 Gate A of the M9 db-split
// (docs/decisions.md Decision D3; ADR
// .claude/knowledge-base/decisions/2026-07-03-m9-db-store-subpackage-topology.md).
//
// Invariant (Gate A): NO file under api/internal/server, api/cmd, an HTTP
// package or a feature's own code imports the sqlc-generated package
// api/internal/db/generated — and, in the
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
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/featurefence"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
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

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

const generatedImportPath = "github.com/getloopscale/loop-runtime/api/internal/db/generated"

// forbiddenDirs are the outer layers that must NOT import the generated
// package. Every HTTP package of the feature layout (docs/decisions.md,
// 2026-09-25) is added to them at scan time, fenced from the hand-written db as
// well, exactly as routes/ was. Relative to RepoRoot. `api/cmd` is the composition root that wires
// the stores — the single highest-leverage place a bypass would be authored,
// so it is fenced alongside the request-handling layers.
var forbiddenDirs = []string{
	"api/internal/server",
	"api/cmd",
}

// handWrittenDBPath is the hand-written persistence package — db.Build,
// db.Release, db.DBTX — as distinct from the sqlc-generated one above.
const handWrittenDBPath = "github.com/getloopscale/loop-runtime/api/internal/db"

// dirsForbiddenFromHandWrittenDB is DELIBERATELY narrower than forbiddenDirs.
//
// B-049's DoD asserts that widening the fence from `db/generated` to `db` is
// "literally a string". Measured 2026-08-12, it is not. Under the semantics this
// gate actually uses — test files INCLUDED, see walkDir — the cost was recorded
// as `api/internal/services` 51 and `api/cmd` 18: 69 files that would fail the
// day the fence widened (71 before slice-6 took routes to zero). A gate that
// fails files on arrival is not a fence; it is an exemption at birth, which is
// the debt slice-1 spent itself removing.
//
// RE-MEASURED 2026-09-04 (B-079), because a count carried forward is the defect
// this fence keeps finding: `api/cmd` is **19**, not 18 — that number was stale
// on arrival and nothing had re-run it. `api/internal/services` is **54**, and
// three of those are this slice's own doing: `delivery.go` left the list when
// `db.Build` and `db.Release` came off the sink, and four files arrived —
// `services/build/activation_build.go`, `services/release/rollback_target.go`
// and their tests — because an adapter that translates a persistence row into a
// contract type has to import both sides. Net **73**, up from the recorded 69.
//
// That is the honest shape of this fence: taking persistence off a CONTRACT does
// not take it out of the layer, it concentrates it at a named boundary. The
// files that would fail on widening went up by four and down by one; what
// changed is that they are now two small adapters instead of a type on an
// interface every consumer inherits.
//
// `api/internal/server` was measured at ZERO, so the fence lands there green and
// permanent: from today the first persistence import into the server layer is
// refused. The other three carry their counts in the backlog rather than an
// exemption nobody can read.
var dirsForbiddenFromHandWrittenDB = []string{
	"api/internal/server",
	// routes/ was added by slice-6, when it reached ZERO importers after db.DBTX
	// — the only symbol it used, in two files — moved to the api/internal/db/dbtx
	// leaf. Since api/internal was organized by feature (2026-09-25) the same
	// fence covers every HTTP package, discovered at scan time.
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

	httpDirs, err := featurefence.HTTPPackageDirs(cfg.RepoRoot)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Outcome{}, err
	}
	dirs := append(append([]string{}, forbiddenDirs...), httpDirs...)
	for _, d := range httpDirs {
		handWritten[d] = true
	}

	for _, sub := range dirs {
		dir := filepath.Join(cfg.RepoRoot, sub)
		if _, err := os.Stat(dir); err != nil {
			// Absent dir is tolerated (partial checkouts / early phases) but
			// recorded — a rename that silently drops a fence must not be
			// invisible.
			missing = append(missing, sub)
			continue
		}
		v, n, err := walkDir(dir, handWritten[sub], nil)
		if err != nil {
			return Outcome{}, err
		}
		all = append(all, v...)
		scanned += n
	}

	// Every feature's own code — its root and non-transport subpackages — is
	// fenced from the generated package, as services/ was. Its HTTP packages
	// were scanned above with both fences; its adapter package is where
	// persistence rows are translated, as adapters/ was, and is not fenced.
	for _, f := range featurefence.Features() {
		dir := filepath.Join(cfg.RepoRoot, "api", "internal", f)
		if _, err := os.Stat(dir); err != nil {
			continue // feature-fence owns the question of a registered feature that is absent
		}
		v, n, err := walkDir(dir, false, skipTransportAndAdapters)
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
		"  · " + generatedImportPath + " — forbidden in server/cmd, the HTTP packages\n" +
		"    and every feature's own code (what services/ held). It\n" +
		"    bypasses the db-store adapter's read-modify-write + advisory-lock scope\n" +
		"    (MA-7 Gate A). Route the access through the entity's Store adapter in\n" +
		"    api/internal/db/store/ instead.\n" +
		"  · " + handWrittenDBPath + " — forbidden in " + strings.Join(dirsForbiddenFromHandWrittenDB, ", ") +
		" and every HTTP package only.\n    That fence is deliberately narrower than the generated one: the other\n" +
		"    layers — the feature services and cmd — carry 73 importers (re-measured 2026-09-04:\n" +
		"    services 54, cmd 19), and a gate that fails on arrival is an exemption at\n" +
		"    birth. Widening it is B-079."
}

// walkDir walks dir for .go files (test files INCLUDED — a test that imports
// generated from an outer layer is still a boundary breach) and collects
// forbidden imports.
func walkDir(dir string, alsoHandWritten bool, skipDir func(name string) bool) ([]Violation, int, error) {
	var violations []Violation
	count := 0

	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			if path != dir && skipDir != nil && skipDir(info.Name()) {
				return filepath.SkipDir
			}
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

// skipTransportAndAdapters leaves out a feature's HTTP packages (scanned on
// their own, with the hand-written fence too) and its adapter package.
func skipTransportAndAdapters(name string) bool {
	return strings.HasSuffix(name, "http") || strings.HasSuffix(name, "adapter")
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
