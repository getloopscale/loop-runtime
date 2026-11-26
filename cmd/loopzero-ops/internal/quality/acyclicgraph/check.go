// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package acyclicgraph is the Go home for the acyclic-graph gate (M5 — plan
// m5-governance-durable). It mechanically enforces the decomposition invariant
// (Robert C. Martin's Acyclic Dependencies Principle): the dependency graph is a
// DAG whose most-stable node (loopzero-contracts) imports nothing, and no consumer is
// imported by a more-stable node.
//
//	loopzero-contracts ◄── loopzero (engine) + loopzero-cloud + loopzero-cli   [acyclic]
//
// The gate scans each module's Go source imports for forbidden upward edges. A
// cycle requires a direct upward import edge somewhere; and "module X imports
// nothing from Y" holds transitively iff no source file of X directly imports Y
// (transitive deps are only reachable THROUGH a direct import) — so scanning
// direct source imports is complete for this invariant and is hermetic + offline
// (no `go list` / build required). Sibling repos (loopzero-contracts, loopzero-cloud) are
// resolved relative to the engine root and SKIPPED when absent.
package acyclicgraph

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// Config tells Check where the engine repo root is. Sibling repos are resolved
// at <RepoRoot>/../loopzero-contracts and <RepoRoot>/../loopzero-cloud.
type Config struct {
	RepoRoot string
}

// Outcome is the gate result.
type Outcome struct {
	Pass       bool
	Violations []string // "node imports forbidden-prefix (file)"
	Skipped    []string // siblings absent (informational)
	Checked    []string // nodes actually scanned
}

// Verdict is Pass when the graph is acyclic and Fail otherwise. An absent
// sibling is informational: it narrows what was checked, and the report says so.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// node is one graph vertex to scan: its directory + the import prefixes it MUST
// NOT contain (the upward edges that would break the DAG).
type node struct {
	name      string
	dir       string
	forbidden []string
}

// skipDir names that never contain first-party source to scan.
var skipDirs = map[string]struct{}{
	"vendor": {}, "node_modules": {}, ".git": {}, "dist": {}, "build": {}, "testdata": {},
}

// Check enforces the acyclic decomposition invariant. The engine node is the
// RepoRoot itself; loopzero-contracts + loopzero-cloud are siblings (skipped when absent).
func Check(cfg Config) (*Outcome, error) {
	parent := filepath.Dir(cfg.RepoRoot)
	nodes := []node{
		{
			name: "loopzero-contracts",
			dir:  filepath.Join(parent, "loopzero-contracts"),
			// The most-stable base imports nothing from any consumer. The engine
			// is github.com/getloopscale/loop-runtime/* (trailing slash → engine only, never
			// matches loopzero-contracts); the leaf consumers are loopzero-cloud + loopzero-cli.
			forbidden: []string{
				"github.com/getloopscale/loop-runtime/",
				"github.com/getloopscale/loopzero-cloud",
				"github.com/getloopscale/loopzero-cli",
			},
		},
		{
			name: "engine",
			dir:  cfg.RepoRoot,
			// The engine never imports the cloud (cloud is a leaf consumer).
			forbidden: []string{
				"github.com/getloopscale/loopzero-cloud",
				"github.com/getloopscale/loopzero-cli",
			},
		},
		{
			name: "loopzero-cloud",
			dir:  filepath.Join(parent, "loopzero-cloud"),
			// The cloud never imports engine-internal packages (only loopzero-contracts).
			forbidden: []string{
				"github.com/getloopscale/loop-runtime/pkg",
				"github.com/getloopscale/loop-runtime/api",
				"github.com/getloopscale/loop-runtime/operators",
				"github.com/getloopscale/loopzero-cli",
			},
		},
		{
			// M6: the CRD types contract module. An in-engine submodule (not a
			// sibling repo) — resolved under RepoRoot, not the parent. It is a
			// clean base: it imports only k8s.io/* and must never reach back into
			// the engine (operators/internal, api, pkg, cloud).
			name: "operators-api",
			dir:  filepath.Join(cfg.RepoRoot, "operators", "api"),
			forbidden: []string{
				"github.com/getloopscale/loop-runtime/operators/internal",
				"github.com/getloopscale/loop-runtime/api",
				"github.com/getloopscale/loop-runtime/pkg",
				"github.com/getloopscale/loopzero-cloud",
				"github.com/getloopscale/loopzero-cli",
			},
		},
	}

	out := &Outcome{Pass: true}
	for _, n := range nodes {
		fi, err := os.Stat(n.dir)
		if err != nil || !fi.IsDir() {
			out.Skipped = append(out.Skipped, n.name)
			continue
		}
		out.Checked = append(out.Checked, n.name)
		violations, err := scanNode(n)
		if err != nil {
			return nil, err
		}
		if len(violations) > 0 {
			out.Pass = false
			out.Violations = append(out.Violations, violations...)
		}
	}
	return out, nil
}

// scanNode walks n.dir's .go files and reports any import matching a forbidden
// prefix. The engine node skips the sibling repos (they live outside RepoRoot,
// so no overlap) and its own loopzero-cloud/loopzero-cli dirs were removed at M2/M4.
func scanNode(n node) ([]string, error) {
	var violations []string
	err := filepath.WalkDir(n.dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if _, skip := skipDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			// The Go toolchain never compiles a directory whose name starts with
			// "." or "_", so nothing in one is an edge of the build graph.
			if path != n.dir && (strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Parse only the import section (AST) so string literals that merely
		// look like import paths (e.g., this gate's own forbidden-prefix list)
		// are NOT mistaken for real imports.
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			// A file whose imports were never read is a hole in the DAG check,
			// not a clean file.
			return fmt.Errorf("%s: cannot parse imports, so its edges are unknown: %w", path, perr)
		}
		for _, spec := range f.Imports {
			imp := strings.Trim(spec.Path.Value, `"`)
			for _, forb := range n.forbidden {
				if strings.HasPrefix(imp, forb) {
					rel := path
					if r, e := filepath.Rel(n.dir, path); e == nil {
						rel = r
					}
					violations = append(violations,
						fmt.Sprintf("%s imports %q (forbidden upward edge: %s* must not be imported by %s) [%s]",
							n.name, imp, forb, n.name, rel))
				}
			}
		}
		return nil
	})
	return violations, err
}
