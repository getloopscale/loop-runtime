// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package servicesacyclic is the architecture fitness function for the M9 Step 6
// bounded-context split of api/internal/services (ADR
// .claude/knowledge-base/decisions/2026-07-03-m9-db-store-subpackage-topology.md;
// MEET-2026-07-03-001). After services was decomposed into eight bounded-context
// subpackages, the inter-context import graph MUST stay the acyclic DAG the split
// produced — a deny-by-default import-linter with an explicit allow-list.
//
// The frozen topology (sink = shared):
//
//	build     → release, scaling, shared
//	release   → shared
//	scaling   → shared
//	preview   → shared
//	domain    → shared
//	platform  → shared
//	placement → (pure leaf)
//	billing   → (pure leaf)
//	shared    → (pure leaf — imports NO other services subpackage)
//
// Any edge not in the allow-list FAILs: a new reverse edge (e.g. release → build)
// would re-introduce a cycle; a new cross-leaf edge (placement → billing) would
// erode the leaf invariant; a new subpackage not registered here forces a
// conscious decision instead of silently widening the graph.
//
// AST-based: only genuine import specs are considered, so a string literal or
// comment mentioning another context's import path is not a false edge.
package servicesacyclic

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Config controls the gate.
type Config struct {
	RepoRoot string
}

// Violation is a forbidden inter-context import edge.
type Violation struct {
	From string // importing subpackage (e.g. "release")
	To   string // imported subpackage (e.g. "build")
	File string
	Line int
}

// Outcome is what the gate returns.
type Outcome struct {
	Pass       bool
	Violations []Violation
	Scanned    int
	// Unknown lists subpackage dirs present on disk but absent from the
	// allow-list — an unregistered context is itself a FAIL (fail-closed).
	Unknown []string
}

// servicesRoot is the decomposed package, relative to RepoRoot.
const servicesRoot = "api/internal/services"

// servicesImportPrefix is the canonical import path of the decomposed package.
const servicesImportPrefix = "github.com/getloopscale/loopzero/api/internal/services/"

// allowed is the deny-by-default adjacency allow-list. allowed[A] is the set of
// subpackages A is permitted to import. A subpackage absent from this map is
// unknown → fail-closed. An empty set means "pure leaf" (imports no sibling).
var allowed = map[string]map[string]bool{
	"build":     {"release": true, "scaling": true, "shared": true},
	"release":   {"shared": true},
	"scaling":   {"shared": true},
	"preview":   {"shared": true},
	"domain":    {"shared": true},
	"platform":  {"shared": true},
	"placement": {},
	"billing":   {},
	"shared":    {},
}

// Check is the gate entry point.
func Check(cfg Config) (Outcome, error) {
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	root := filepath.Join(cfg.RepoRoot, servicesRoot)
	if _, err := os.Stat(root); err != nil {
		// services/ absent (partial checkout) — trivially passes.
		return Outcome{Pass: true}, nil
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return Outcome{}, err
	}

	var violations []Violation
	var unknown []string
	scanned := 0

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ctx := e.Name()
		allowedTargets, known := allowed[ctx]
		if !known {
			unknown = append(unknown, ctx)
			continue
		}
		dir := filepath.Join(root, ctx)
		v, n, err := scanContext(dir, ctx, allowedTargets)
		if err != nil {
			return Outcome{}, err
		}
		violations = append(violations, v...)
		scanned += n
	}

	sort.Strings(unknown)
	pass := len(violations) == 0 && len(unknown) == 0
	return Outcome{Pass: pass, Violations: violations, Scanned: scanned, Unknown: unknown}, nil
}

// scanContext parses every .go file in one subpackage dir and flags any import of
// another services subpackage not in allowedTargets. Test files are included:
// the acyclic invariant must hold for test imports too (a test that imports a
// forbidden sibling still creates a build-time cycle risk).
func scanContext(dir, ctx string, allowedTargets map[string]bool) ([]Violation, int, error) {
	var violations []Violation
	scanned := 0
	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			// Descend into nested subpackages too: a future
			// services/<ctx>/internal/... that imports a forbidden sibling must
			// still be caught — the parent context's allow-list applies to its
			// whole subtree, else the gate is trivially bypassable by nesting.
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".go") {
			return nil
		}
		scanned++
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(p, servicesImportPrefix) {
				continue
			}
			target := strings.TrimPrefix(p, servicesImportPrefix)
			// Only the first path segment is the sibling context.
			if i := strings.IndexByte(target, '/'); i >= 0 {
				target = target[:i]
			}
			if target == ctx {
				continue // self-import (aliased) — not a sibling edge.
			}
			if !allowedTargets[target] {
				pos := fset.Position(imp.Pos())
				violations = append(violations, Violation{From: ctx, To: target, File: path, Line: pos.Line})
			}
		}
		return nil
	})
	return violations, scanned, err
}

// ActionableFix returns the operator-facing message for FAIL output.
func ActionableFix() string {
	return "An inter-context import edge violates the frozen acyclic DAG of " +
		"api/internal/services (M9 Step 6). Either (a) the dependency belongs in " +
		"the shared/ leaf — move the shared symbol there; or (b) the edge is a " +
		"genuine new dependency that must be reviewed and added to the allow-list " +
		"in servicesacyclic/check.go with an ADR justifying why it does not create " +
		"a cycle. Reverse edges (e.g. release importing build) are forbidden — " +
		"they re-introduce the cycle the split removed."
}
