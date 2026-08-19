// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package servicesacyclic is the architecture fitness function for the M9 Step 6
// bounded-context split of api/internal/services (ADR
// .claude/knowledge-base/decisions/2026-07-03-m9-db-store-subpackage-topology.md;
// MEET-2026-07-03-001). After services was decomposed into eight bounded-context
// subpackages, the inter-context import graph MUST stay the acyclic DAG the split
// produced — a deny-by-default import-linter with an explicit allow-list.
//
// The frozen topology (sink = deploycontracts):
//
//	build     → release, scaling, deploycontracts
//	release   → deploycontracts
//	scaling   → deploycontracts
//	preview   → deploycontracts
//	domain    → deploycontracts
//	platform  → deploycontracts
//	placement → (pure leaf)
//	deploycontracts    → (pure leaf — imports NO other services subpackage)
//
// Any edge not in the allow-list FAILs: a new reverse edge (e.g. release → build)
// would re-introduce a cycle; a new cross-leaf edge (placement → deploycontracts) would
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

// missingRootMarker is the Unknown[] sentinel emitted when the services root is
// renamed away or empty — a fail-closed signal distinct from a per-context name.
// It cannot collide with a real subpackage (dir names never contain '<').
const missingRootMarker = "<services-root-missing-or-empty>"

// `billing` NAO esta aqui, e a ausencia e' deliberada (B-065). O dominio foi
// extraido para `../loopzero-cloud` no M4; `api/internal/services/billing` ficou como
// diretorio VAZIO e nao rastreado, e o registro sobreviveu ao codigo. Um contexto
// registrado para codigo inexistente GASTA ADIANTADO a decisao que este gate
// existe para forcar: quem criasse o diretorio depois comecaria a importar sem
// que nada perguntasse. A rota `GET /api/v1/billing` continua servida deste
// repositorio (`api/internal/server/router.go:610`) delegando ao handler; a
// LOGICA de billing mora no `loopzero-cloud`. Se um dia existir codigo de billing
// aqui, registrar de novo — conscientemente.
//
// allowed is the deny-by-default adjacency allow-list. allowed[A] is the set of
// subpackages A is permitted to import. A subpackage absent from this map is
// unknown → fail-closed. An empty set means "pure leaf" (imports no sibling).
var allowed = map[string]map[string]bool{
	"build":           {"release": true, "scaling": true, "deploycontracts": true},
	"release":         {"deploycontracts": true},
	"scaling":         {"deploycontracts": true},
	"preview":         {"deploycontracts": true},
	"domain":          {"deploycontracts": true},
	"platform":        {"deploycontracts": true},
	"placement":       {},
	"deploycontracts": {},
}

// Check is the gate entry point.
func Check(cfg Config) (Outcome, error) {
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	root := filepath.Join(cfg.RepoRoot, servicesRoot)
	info, err := os.Stat(root)
	if err != nil {
		if !os.IsNotExist(err) {
			// Permission or other stat error — surface it, never silently pass.
			return Outcome{}, fmt.Errorf("stat %s: %w", root, err)
		}
		// services/ is absent. Distinguish a genuine partial checkout (the parent
		// api/internal/ is also absent → nothing to enforce → skip) from a
		// renamed-away services root (parent present, services/ gone → a real
		// regression this gate exists to catch → fail-closed). Pre-M9 §7-step-1
		// this branch returned Pass:true unconditionally — a false green that let
		// a destroyed decomposition ship past CI.
		parent := filepath.Join(cfg.RepoRoot, filepath.Dir(servicesRoot))
		if _, perr := os.Stat(parent); perr != nil {
			if os.IsNotExist(perr) {
				return Outcome{Pass: true}, nil // partial checkout — trivially skip.
			}
			return Outcome{}, fmt.Errorf("stat %s: %w", parent, perr)
		}
		return Outcome{Pass: false, Unknown: []string{missingRootMarker}}, nil
	}
	if !info.IsDir() {
		return Outcome{}, fmt.Errorf("%s is not a directory", root)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return Outcome{}, err
	}

	var violations []Violation
	var unknown []string
	scanned := 0
	knownContexts := 0

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
		knownContexts++
		dir := filepath.Join(root, ctx)
		v, n, err := scanContext(dir, ctx, allowedTargets)
		if err != nil {
			return Outcome{}, err
		}
		violations = append(violations, v...)
		scanned += n
	}

	// An existing-but-empty services root (zero known contexts, nothing unknown to
	// explain it) means the whole decomposition vanished — fail-closed with a
	// self-documenting sentinel rather than passing on an empty graph.
	if knownContexts == 0 && len(unknown) == 0 {
		unknown = append(unknown, missingRootMarker)
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
		"the deploycontracts/ leaf — move the shared symbol there; or (b) the edge is a " +
		"genuine new dependency that must be reviewed and added to the allow-list " +
		"in servicesacyclic/check.go with an ADR justifying why it does not create " +
		"a cycle. Reverse edges (e.g. release importing build) are forbidden — " +
		"they re-introduce the cycle the split removed."
}
