// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package subpackagefence is the one import-fence engine behind the gates that
// freeze how the subpackages of a Go package may import each other
// (feature-fence; services-acyclic and routes-acyclic used it until
// api/internal was organized by feature).
//
// A fence is deny-by-default. Every subpackage must be registered; a registered
// subpackage may import only the siblings its allow-list names; nobody imports the
// parent package from below; the parent may import only declared leaves; and a
// test-only leaf may be reached from _test.go files alone. The registry itself is
// checked before the tree: it must be non-empty, name only registered targets,
// keep leaves free of edges, and be acyclic — an allow-list that permits a cycle
// would approve the very coupling the fence exists to forbid.
//
// The rules live here once. Each gate that uses the engine is a registry plus the
// wording of its fix.
package subpackagefence

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ParentNode names the parent package in an Edge.
const ParentNode = "<parent>"

// Why a fence refuses. Each is wrapped with the fence's directory.
var (
	ErrEmptyAllowList             = errors.New("the registered subpackage set is empty — a fence that does not know what it permits has verified nothing")
	ErrIncoherentRegistry         = errors.New("the registry contradicts itself")
	ErrRegistryCycle              = errors.New("the allow-list permits an import cycle — the registry approves the coupling the fence exists to forbid")
	ErrUnregisteredSubpackage     = errors.New("a subpackage is not in the registered set — nobody decided what it may import")
	ErrForbiddenEdge              = errors.New("a subpackage reaches one it must not — neither a sibling outside its allow-list nor the parent package may be imported")
	ErrTestOnlyLeafFromProduction = errors.New("production code imports a test-only leaf")
	ErrNothingParsed              = errors.New("no Go file was parsed — the gate examined nothing, so it cannot vouch for anything")
)

// Fence describes one package whose subpackages are fenced.
type Fence struct {
	// Dir is the parent package directory, relative to the repository root.
	Dir string
	// PkgPath is the parent package import path; subpackages are PkgPath + "/<name>".
	PkgPath string
	// Allowed maps each registered subpackage to the siblings it may import.
	Allowed map[string]map[string]bool
	// Leaves are subpackages the parent package may import. A leaf imports no
	// sibling.
	Leaves map[string]bool
	// TestOnlyLeaves are leaves that only _test.go files may import.
	TestOnlyLeaves map[string]bool
}

// Edge is a forbidden import.
type Edge struct {
	From string
	To   string
	File string
	Line int
	// TestOnly marks a production file importing a test-only leaf — a different
	// fix from a coupling edge.
	TestOnly bool
}

// Outcome is what one scan established.
type Outcome struct {
	Subpackages  []string
	Unregistered []string
	Edges        []Edge
	FilesParsed  int
}

// Check validates the registry, then scans repoRoot/f.Dir.
func Check(repoRoot string, f Fence) (Outcome, error) {
	var out Outcome
	if repoRoot == "" {
		repoRoot = "."
	}
	if err := ValidateRegistry(f); err != nil {
		return out, fmt.Errorf("%s: %w", f.Dir, err)
	}

	root := filepath.Join(repoRoot, f.Dir)
	entries, err := os.ReadDir(root)
	if err != nil {
		return out, fmt.Errorf("reading %s: %w", f.Dir, err)
	}
	for _, e := range entries {
		if !e.IsDir() || skippedDir(e.Name()) {
			continue
		}
		name := e.Name()
		dir := filepath.Join(root, name)
		// A directory holding no Go file is not a subpackage: it exists on the disk
		// of whoever built here and not in a fresh clone, so counting it would make
		// the verdict depend on the machine.
		hasGo, err := containsGo(dir)
		if err != nil {
			return out, err
		}
		if !hasGo {
			continue
		}
		out.Subpackages = append(out.Subpackages, name)
		permitted, registered := f.Allowed[name]
		if !registered {
			out.Unregistered = append(out.Unregistered, name)
			continue
		}
		edges, n, err := f.scanSubpackage(dir, name, permitted)
		if err != nil {
			return out, err
		}
		out.Edges = append(out.Edges, edges...)
		out.FilesParsed += n
	}

	parentEdges, n, err := f.scanParent(root, entries)
	if err != nil {
		return out, err
	}
	out.Edges = append(out.Edges, parentEdges...)
	out.FilesParsed += n

	sort.Strings(out.Subpackages)
	sort.Strings(out.Unregistered)
	return out, out.verdict(f.Dir)
}

func (o Outcome) verdict(dir string) error {
	if o.FilesParsed == 0 {
		return fmt.Errorf("%s: %w", dir, ErrNothingParsed)
	}
	if len(o.Unregistered) > 0 {
		return fmt.Errorf("%s: %w: %s", dir, ErrUnregisteredSubpackage, strings.Join(o.Unregistered, ", "))
	}
	if len(o.Edges) == 0 {
		return nil
	}
	for _, e := range o.Edges {
		if !e.TestOnly {
			return fmt.Errorf("%s: %w", dir, ErrForbiddenEdge)
		}
	}
	return fmt.Errorf("%s: %w", dir, ErrTestOnlyLeafFromProduction)
}

// ValidateRegistry checks the registry before any file is read.
func ValidateRegistry(f Fence) error {
	if len(f.Allowed) == 0 {
		return ErrEmptyAllowList
	}
	var bad []string
	for _, name := range sortedKeys(f.Allowed) {
		for _, target := range sortedKeys(f.Allowed[name]) {
			if _, ok := f.Allowed[target]; !ok {
				bad = append(bad, fmt.Sprintf("%s permits %s, which is not registered", name, target))
			}
		}
		if f.Leaves[name] && len(f.Allowed[name]) > 0 {
			bad = append(bad, fmt.Sprintf("%s is declared a leaf and also permits siblings", name))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("%w: %s", ErrIncoherentRegistry, strings.Join(bad, "; "))
	}
	if cycle := findCycle(f.Allowed); cycle != nil {
		return fmt.Errorf("%w: %s", ErrRegistryCycle, strings.Join(cycle, " → "))
	}
	return nil
}

// findCycle returns one cycle in the allow-list graph (first node repeated at the
// end), or nil when the graph is a DAG. Iteration is over sorted keys so the
// reported cycle is deterministic.
func findCycle(graph map[string]map[string]bool) []string {
	const (
		unvisited = iota
		onStack
		done
	)
	state := map[string]int{}
	var stack []string
	var visit func(string) []string
	visit = func(n string) []string {
		state[n] = onStack
		stack = append(stack, n)
		for _, next := range sortedKeys(graph[n]) {
			switch state[next] {
			case onStack:
				for i, s := range stack {
					if s == next {
						return append(append([]string{}, stack[i:]...), next)
					}
				}
			case unvisited:
				if c := visit(next); c != nil {
					return c
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[n] = done
		return nil
	}
	for _, n := range sortedKeys(graph) {
		if state[n] == unvisited {
			if c := visit(n); c != nil {
				return c
			}
		}
	}
	return nil
}

// scanSubpackage parses every Go file under one registered subpackage, nested
// directories included — the subpackage's allow-list governs its whole subtree,
// or nesting would bypass the fence. _test.go files are included: a test import
// is still a build edge.
func (f Fence) scanSubpackage(dir, self string, permitted map[string]bool) ([]Edge, int, error) {
	var edges []Edge
	count := 0
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path != dir && skippedDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		imports, err := parseImports(fset, path)
		if err != nil {
			return err
		}
		count++
		for _, imp := range imports {
			edge := Edge{From: self, File: path, Line: imp.line}
			if imp.path == f.PkgPath {
				edge.To = ParentNode
				edges = append(edges, edge)
				continue
			}
			target, ok := f.subpackageOf(imp.path)
			if !ok {
				continue
			}
			edge.To = target
			if f.TestOnlyLeaves[target] && isProduction(d.Name()) {
				edge.TestOnly = true
				edges = append(edges, edge)
				continue
			}
			if target == self || permitted[target] {
				continue
			}
			edges = append(edges, edge)
		}
		return nil
	})
	return edges, count, err
}

// scanParent reads the Go files directly in the parent directory. The parent may
// import leaves only, and a test-only leaf only from a _test.go.
func (f Fence) scanParent(root string, entries []os.DirEntry) ([]Edge, int, error) {
	var edges []Edge
	count := 0
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		path := filepath.Join(root, e.Name())
		imports, err := parseImports(fset, path)
		if err != nil {
			return nil, 0, err
		}
		count++
		for _, imp := range imports {
			target, ok := f.subpackageOf(imp.path)
			if !ok {
				continue
			}
			testOnlyFromProduction := f.TestOnlyLeaves[target] && isProduction(e.Name())
			if f.Leaves[target] && !testOnlyFromProduction {
				continue
			}
			edges = append(edges, Edge{From: ParentNode, To: target, File: path, Line: imp.line, TestOnly: testOnlyFromProduction})
		}
	}
	return edges, count, nil
}

// subpackageOf returns the first path segment below PkgPath, the subpackage an
// import belongs to.
func (f Fence) subpackageOf(importPath string) (string, bool) {
	rest, ok := strings.CutPrefix(importPath, f.PkgPath+"/")
	if !ok || rest == "" {
		return "", false
	}
	target, _, _ := strings.Cut(rest, "/")
	return target, true
}

type goImport struct {
	path string
	line int
}

// parseImports reads the import specs of one file. Matching on the import PATH
// rather than on a local name keeps aliased, dot and blank imports visible.
func parseImports(fset *token.FileSet, path string) ([]goImport, error) {
	file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	imports := make([]goImport, 0, len(file.Imports))
	for _, spec := range file.Imports {
		if spec.Path == nil {
			continue
		}
		imports = append(imports, goImport{
			path: strings.Trim(spec.Path.Value, "\"`"),
			line: fset.Position(spec.Pos()).Line,
		})
	}
	return imports, nil
}

// containsGo reports whether dir holds a .go file at any depth — a subpackage may
// keep its files in a nested directory.
func containsGo(dir string) (bool, error) {
	found := false
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".go") {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found, err
}

// skippedDir mirrors the go tool: testdata and dot/underscore directories are not
// packages.
func skippedDir(name string) bool {
	return name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

func isProduction(fileName string) bool { return !strings.HasSuffix(fileName, "_test.go") }

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
