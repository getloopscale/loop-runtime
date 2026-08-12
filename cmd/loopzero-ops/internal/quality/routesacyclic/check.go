// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package routesacyclic is the fence that must exist BEFORE `api/internal/routes`
// is split into per-family subpackages.
//
// # Why it exists before there is anything to fence
//
// `B-041` states the order explicitly: the fence precedes the move. A fence
// written afterwards is written to fit what was already done, and stops being a
// constraint the moment it is authored. Registering the permitted set first means
// the FIRST subpackage to exist is born declared rather than discovered later.
//
// The design is a deliberate clone of `servicesacyclic`, which froze the nine
// services contexts and has been wired and green for months. `B-041`'s case for
// splitting routes rests on that precedent: the decomposition is propagation of a
// ratified pattern, not the introduction of a new one.
//
// # What it refuses
//
//  1. A subpackage of `routes` that is not in the registered set. Deny by
//     default: an unknown directory is a handler family nobody decided on.
//  2. An import edge between two sibling subpackages. Families are peers; a
//     dependency between them is the coupling the split exists to remove.
//
// # Why the transport package is registered as a leaf
//
// `httpio` holds the three response writers every family needs. Before slice-5
// they lived in `projects.go`, so every future family would have depended on the
// `projects` family to render a response — the exact edge rule 2 forbids. It is
// registered with an empty allowed-set: everyone may import it, it imports no
// sibling.
package routesacyclic

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrUnregisteredSubpackage means a directory exists under routes/ that the
// allow-list does not name.
var ErrUnregisteredSubpackage = errors.New("a subpackage of api/internal/routes is not in the registered set — a handler family nobody decided on")

// ErrSiblingEdge means one family imports another.
var ErrSiblingEdge = errors.New("one routes subpackage imports a sibling — families are peers, and an edge between them is the coupling the split exists to remove")

// ErrEmptyAllowList means the registered set is empty. A fence that does not
// know what it permits permits nothing on purpose, and passing green in that
// state is the vacuous-pass shape B-040 measured.
var ErrEmptyAllowList = errors.New("the registered subpackage set is empty — a fence that does not know what it permits has verified nothing")

const routesDir = "api/internal/routes"
const routesPkgPath = "github.com/getloopscale/loopzero/api/internal/routes"
const routesImportPrefix = routesPkgPath + "/"

// allowed is the deny-by-default adjacency allow-list, in the shape
// servicesacyclic established. allowed[A] is the set of siblings A may import.
// An empty set means pure leaf.
//
// It starts with the one subpackage that exists. Every family added by the split
// (B-080) adds its own entry, which is the point: the addition is a declaration,
// not a discovery.
var allowed = map[string]map[string]bool{
	"httpio": {},
	// The first real handler family (slice-8, B-041). Empty set = imports no
	// sibling: it does not even use httpio, which is what made it the honest
	// first proof of the recipe. Serves /api/v1/version and /v1/version.
	"version": {},
}

// Config drives the gate.
type Config struct {
	RepoRoot string
	// Allowed overrides the registry. Tests only.
	Allowed map[string]map[string]bool
}

// Edge is one sibling import that must not exist.
type Edge struct {
	From string
	To   string
	File string
	Line int
}

// Outcome is the summary.
type Outcome struct {
	Subpackages  []string
	Unregistered []string
	Edges        []Edge
	FilesParsed  int
}

// Check walks the routes subpackages and enforces both rules.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	reg := cfg.Allowed
	if reg == nil {
		reg = allowed
	}
	if len(reg) == 0 {
		return out, ErrEmptyAllowList
	}

	root := filepath.Join(cfg.RepoRoot, routesDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		// The routes package itself being gone is a rename that must be loud, not
		// a silent green: the fence would otherwise vanish with its subject.
		return out, fmt.Errorf("reading %s: %w", routesDir, err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// Directories the Go toolchain itself ignores are not subpackages, and
		// reporting one as an unregistered family would be a finding about a
		// convention rather than about the code.
		if name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		out.Subpackages = append(out.Subpackages, name)
		siblings, registered := reg[name]
		if !registered {
			out.Unregistered = append(out.Unregistered, name)
			continue
		}
		edges, n, err := scanPackage(filepath.Join(root, name), name, siblings)
		if err != nil {
			return out, err
		}
		out.Edges = append(out.Edges, edges...)
		out.FilesParsed += n
	}

	sort.Strings(out.Subpackages)
	sort.Strings(out.Unregistered)

	if len(out.Unregistered) > 0 {
		return out, ErrUnregisteredSubpackage
	}
	if len(out.Edges) > 0 {
		return out, ErrSiblingEdge
	}
	return out, nil
}

// scanPackage parses every .go file in one subpackage and records imports of
// siblings that the allow-list does not permit.
//
// Test files are INCLUDED, matching servicesacyclic and dbgeneratedboundary: a
// test that reaches across a family boundary is still an edge, and excluding
// tests would leave the cheapest place to author a bypass unwatched.
func scanPackage(dir, self string, permitted map[string]bool) ([]Edge, int, error) {
	var edges []Edge
	count := 0
	fset := token.NewFileSet()

	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			// Descend. A family's allow-list applies to its WHOLE subtree: an
			// edge authored one level down — secrets/store/y.go importing
			// routes/builds — is the same violation as one at the top.
			//
			// The first version of this file used a non-recursive ReadDir and
			// that fixture passed GREEN. servicesacyclic, which this package's
			// doc calls a deliberate clone, carries the lesson verbatim: "else
			// the gate is trivially bypassable by nesting". The clone had lost
			// the one line that mattered, and the asymmetry made the hole look
			// covered — the TARGET side already resolved nested paths, so a test
			// for nesting existed and passed while the SOURCE side never walked.
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		count++
		for _, imp := range f.Imports {
			if imp.Path == nil {
				continue
			}
			p := strings.Trim(imp.Path.Value, `"`)
			// The parent package itself is watched too: after the split, a family
			// reaching back for a helper left behind in routes/ is exactly the
			// coupling the split removes, and a prefix with a trailing slash
			// would never see it.
			if p == routesPkgPath {
				edges = append(edges, Edge{From: self, To: "<routes-parent>", File: path, Line: fset.Position(imp.Pos()).Line})
				continue
			}
			if !strings.HasPrefix(p, routesImportPrefix) {
				continue
			}
			// The sibling is the FIRST path element after the prefix: a nested
			// package under a family still belongs to that family.
			target := strings.SplitN(strings.TrimPrefix(p, routesImportPrefix), "/", 2)[0]
			if target == self || permitted[target] {
				continue
			}
			// A registered sibling that is not permitted and a target nobody
			// registered are the same finding here: an edge that must not exist,
			// reported with the file and line that reaches for it.
			edges = append(edges, Edge{From: self, To: target, File: path, Line: fset.Position(imp.Pos()).Line})
		}
		return nil
	})
	return edges, count, err
}

// Summary renders the one-line verdict.
func (o Outcome) Summary() string {
	return fmt.Sprintf("%d subpackage(s) under %s, %d file(s) parsed, %d unregistered, %d sibling edge(s)",
		len(o.Subpackages), routesDir, o.FilesParsed, len(o.Unregistered), len(o.Edges))
}

// ActionableFix is the operator-facing message for a FAIL.
func ActionableFix() string {
	return "Either the subpackage is a handler family that was never decided on — in\n" +
		"which case decide it and add it to the allow-list with the router family it\n" +
		"serves — or one family reached for another. Families are peers: shared code\n" +
		"belongs in a leaf every family may import, the way httpio holds the response\n" +
		"writers that used to live inside the projects family."
}
