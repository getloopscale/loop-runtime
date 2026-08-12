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
//  3. The PARENT package importing a family. Same coupling, on the vertical.
//
// # A leaf yes, a family no
//
// Rule 3 does not forbid every parent edge — it forbids reaching a FAMILY. A leaf
// (a subpackage whose allowed set is empty) exists to be imported by everyone,
// and the parent is part of everyone: measured before this rule landed, the
// parent held 29 edges and ALL of them went to leaves — 28 to httpio, 1 to
// routestest. Forbidding them all would have failed 29 files the day the fence
// arrived, which is an exemption at birth.
//
// A leaf is DECLARED, not derived. The first attempt derived it from the empty
// allowed set and was wrong: `version: {}` is a FAMILY that imports no sibling,
// and `httpio: {}` is a leaf — indistinguishable by that rule, so the parent
// could reach `version` and the gate would call it a leaf. The plan had written
// exactly this risk (R2) before the code repeated it.
//
// # Why the transport package is registered as a leaf
//
// `httpio` holds the response writers every family needs — three moved out of a
// domain file in slice-5, a fourth surfaced when the tenants family split. Before slice-5
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
var ErrSiblingEdge = errors.New("a routes package reaches a family it must not — families are peers, and neither a sibling nor the parent package may depend on one; that is the coupling the split exists to remove")

// ErrEmptyAllowList means the registered set is empty. A fence that does not
// know what it permits permits nothing on purpose, and passing green in that
// state is the vacuous-pass shape B-040 measured.
// ErrIncoherentRegistry means the two hand-maintained maps disagree: a name is
// declared BOTH as a family (an entry in allowed with siblings) and as a leaf.
//
// Nothing detected this until it was measured: declaring `tenants` a leaf while
// it carried `{httpio, routestest}` made the parent reach it and the gate pass.
// The plan had rejected the explicit-leaf design on exactly this ground —
// "deriving instead of repeating is what stops the two from diverging" — and the
// bug that forced the explicit design then reopened the risk the rejection named.
// Two maps maintained by hand across 14 more family moves need the check.
// ErrNothingParsed means the walk read no Go file at all. The gate examined
// nothing, so it cannot vouch for anything.
//
// The plan promised this floor and the first implementation omitted it —
// measured: an existing-but-empty routes/ reported `0 file(s) parsed` and PASS,
// while servicesacyclic, which this package calls a deliberate clone, fails
// closed on exactly that shape and says why in its own source. It is the third
// lesson this lineage lost from that file; the second was filepath.Walk.
//
// The omission was material, not cosmetic: making scanParent skip _test.go — a
// plausible optimisation — drops the real read from 97 files to 49 and the whole
// suite stays green. The parent's only routestest edge lives in a _test.go.
var ErrNothingParsed = errors.New("no Go file was parsed under api/internal/routes — the gate examined nothing, so it cannot vouch for anything")

var ErrIncoherentRegistry = errors.New("a name is declared both as a family and as a leaf — the two registries disagree, and the parent would reach the family believing it a leaf")

var ErrEmptyAllowList = errors.New("the registered subpackage set is empty — a fence that does not know what it permits has verified nothing")

const routesDir = "api/internal/routes"
const routesPkgPath = "github.com/getloopscale/loopzero/api/internal/routes"
const routesImportPrefix = routesPkgPath + "/"

// allowed is the deny-by-default adjacency allow-list, in the shape
// servicesacyclic established. allowed[A] is the set of siblings A may import.
// An empty set means "imports no sibling" — which a LEAF has, and so does a
// family that happens to depend on none. That ambiguity was the bug: see the
// package doc, and the `leaves` registry below, which declares leafness instead
// of inferring it.
//
// It starts with the one subpackage that exists. Every family added by the split
// (B-080) adds its own entry, which is the point: the addition is a declaration,
// not a discovery.
// leaves are the subpackages every family — and the parent — may import. A leaf
// serves no router prefix; it exists to be depended upon.
//
// Declared rather than derived: see the package doc. A family with no permitted
// sibling has the same empty set as a leaf, and treating them alike let the
// parent reach a family unchecked.
var leaves = map[string]bool{
	"httpio":     true, // response writers (slice-5)
	"routestest": true, // test request helpers (slice-8)
}

var allowed = map[string]map[string]bool{
	"httpio": {},
	// The first real handler family (slice-8, B-041). Empty set = imports no
	// sibling: it does not even use httpio, which is what made it the honest
	// first proof of the recipe. Serves /api/v1/version and /v1/version.
	"version": {},
	// The second family (slice-8). Unlike version, it DOES depend on the shared
	// leaf — `httpio: true` is the first permitted edge this fence has ever
	// carried, and declaring it is what makes the leaf design real rather than
	// hypothetical: every family may reach the transport, none may reach a peer.
	"tenants": {"httpio": true, "routestest": true},
	// The test-side leaf. Helpers in a _test.go are compiled only into their own
	// package's test binary, so a family that moves out loses them — measured the
	// instant `tenants` moved and its test failed on `undefined: withAuth`, a
	// helper THIRTY test files share. Empty set: every family may import it, it
	// imports none.
	"routestest": {},
}

// Config drives the gate.
type Config struct {
	RepoRoot string
	// Allowed overrides the registry. Tests only.
	Allowed map[string]map[string]bool
	// Leaves overrides the declared leaf set. Tests only.
	Leaves map[string]bool
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

	// The parent package itself, which Check ignored until B-084. Measured then:
	// the parent held 89 .go files and the gate read 8 — a probe importing a
	// family passed green, and fourteen families were still to move.
	leafSet := cfg.Leaves
	if leafSet == nil {
		leafSet = leaves
	}
	// The two registries must agree BEFORE anything is scanned: a name declared
	// both ways makes every later verdict about it meaningless.
	if bad := incoherent(reg, leafSet); len(bad) > 0 {
		return out, fmt.Errorf("%w: %s", ErrIncoherentRegistry, strings.Join(bad, ", "))
	}
	parentEdges, parentFiles, err := scanParent(root, leafSet)
	if err != nil {
		return out, err
	}
	out.Edges = append(out.Edges, parentEdges...)
	out.FilesParsed += parentFiles

	sort.Strings(out.Subpackages)
	sort.Strings(out.Unregistered)

	// The floor comes AFTER the findings: when everything is a violation the
	// counts alone cannot tell "worst possible result" from "measured nothing".
	if out.FilesParsed == 0 {
		return out, ErrNothingParsed
	}
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
			// Directories the Go toolchain itself ignores are not part of the
			// family: a fixture under testdata/ is never compiled, so an import
			// there is not an edge. The top-level walk already skips them and
			// this side did not, which would have made the first family with a
			// fixture an exemption.
			if n := info.Name(); n != "." && (n == "testdata" || strings.HasPrefix(n, ".") || strings.HasPrefix(n, "_")) {
				return filepath.SkipDir
			}
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

// incoherent returns the names declared both as a family and as a leaf.
//
// A leaf serves no router prefix and imports nothing; a family does the
// opposite. A name in both registries is a contradiction, and the one that wins
// silently is the leaf — which is the lenient side, so the mistake opens the
// fence rather than closing it.
func incoherent(reg map[string]map[string]bool, leafSet map[string]bool) []string {
	var bad []string
	for name, siblings := range reg {
		if !leafSet[name] {
			continue
		}
		// A leaf legitimately appears in allowed with an EMPTY set — that is how
		// httpio and routestest are registered. The contradiction is a leaf that
		// also declares siblings.
		if len(siblings) > 0 {
			bad = append(bad, name)
		}
	}
	sort.Strings(bad)
	return bad
}

// scanParent reads the .go files sitting directly in routes/ and records any
// import that reaches a FAMILY. Reaching a leaf is permitted — see the package
// doc for why the distinction is derived from the empty allowed set.
func scanParent(root string, leafSet map[string]bool) ([]Edge, int, error) {
	var edges []Edge
	count := 0
	fset := token.NewFileSet()

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, 0, fmt.Errorf("reading %s: %w", routesDir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		path := filepath.Join(root, e.Name())
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return nil, 0, fmt.Errorf("parsing %s: %w", path, err)
		}
		count++
		for _, imp := range f.Imports {
			if imp.Path == nil {
				continue
			}
			p := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(p, routesImportPrefix) {
				continue
			}
			target := strings.SplitN(strings.TrimPrefix(p, routesImportPrefix), "/", 2)[0]
			// A leaf is importable by everyone, the parent included. Anything
			// else — a family, or a directory nobody registered — is an edge,
			// and naming the file that reaches for it is more useful than
			// staying silent.
			if leafSet[target] {
				continue
			}
			edges = append(edges, Edge{From: "<routes-parent>", To: target, File: path, Line: fset.Position(imp.Pos()).Line})
		}
	}
	return edges, count, nil
}

// Summary renders the one-line verdict.
func (o Outcome) Summary() string {
	return fmt.Sprintf("%d subpackage(s) under %s, %d file(s) parsed, %d unregistered, %d sibling edge(s)",
		len(o.Subpackages), routesDir, o.FilesParsed, len(o.Unregistered), len(o.Edges))
}

// ActionableFix is the operator-facing message for a FAIL.
func ActionableFix() string {
	return "One of three things:\n" +
		"  · a subpackage nobody registered — decide the family and add it to the\n" +
		"    allow-list with the router prefix it serves;\n" +
		"  · one family reached for another — families are peers;\n" +
		"  · the PARENT package reached for a family — same coupling, on the\n" +
		"    vertical, and the usual cause is a helper that both need.\n" +
		"In the last two cases the fix is the same: move the shared code into a leaf\n" +
		"every family may import, the way httpio holds the response writers that used\n" +
		"to live inside the projects family, and routestest holds the request helpers\n" +
		"that used to live in a _test.go of the parent."
}
