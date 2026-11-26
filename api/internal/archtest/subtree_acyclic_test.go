// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package archtest holds architecture tests: assertions about how the packages
// under api/internal depend on each other, not about what any of them does.
//
// # Why the subtree, not the package
//
// The Go compiler already refuses an import cycle between two packages. It
// cannot see a cycle between two DIRECTORY TREES: server/ imports metrics/,
// and metrics/ imports server/surfaces — two different packages, so the build
// is green, while the two trees can no longer be reasoned about, tested or
// moved apart. The 2026-09-23 architecture review measured 12 api/internal
// subtrees in one strongly connected component this way. This test is the
// check the compiler does not make.
package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/getloopscale/loop-runtime/api"

// internalRoot is api/internal, resolved from this file's directory.
func internalRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Dir(wd)
}

// subtreeOf maps "github.com/getloopscale/loop-runtime/api/internal/builds/argo" to
// "builds". It returns "" for anything outside api/internal.
func subtreeOf(importPath string) string {
	prefix := modulePath + "/internal/"
	if !strings.HasPrefix(importPath, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(importPath, prefix)
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return rest[:i]
	}
	return rest
}

// subtreeGraph returns, for every api/internal subtree, the set of OTHER
// subtrees its production (non-test) files import, with one import site each
// so a failure names the line to change.
func subtreeGraph(t *testing.T) map[string]map[string]string {
	t.Helper()
	root := internalRoot(t)
	graph := map[string]map[string]string{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		from := strings.Split(filepath.ToSlash(rel), "/")[0]
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			target, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			to := subtreeOf(target)
			if to == "" || to == from {
				continue
			}
			if graph[from] == nil {
				graph[from] = map[string]string{}
			}
			if _, seen := graph[from][to]; !seen {
				graph[from][to] = fset.Position(imp.Pos()).String()
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return graph
}

// stronglyConnected returns every strongly connected component with more than
// one subtree (Tarjan). A component of size one is not a cycle: self-imports
// were dropped when the graph was built.
func stronglyConnected(graph map[string]map[string]string) [][]string {
	index, low := map[string]int{}, map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var components [][]string
	next := 0

	nodes := make([]string, 0, len(graph))
	for n := range graph {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)

	var visit func(string)
	visit = func(v string) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		onStack[v] = true
		targets := make([]string, 0, len(graph[v]))
		for w := range graph[v] {
			targets = append(targets, w)
		}
		sort.Strings(targets)
		for _, w := range targets {
			if _, visited := index[w]; !visited {
				visit(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] != index[v] {
			return
		}
		var component []string
		for {
			w := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[w] = false
			component = append(component, w)
			if w == v {
				break
			}
		}
		if len(component) > 1 {
			sort.Strings(component)
			components = append(components, component)
		}
	}
	for _, n := range nodes {
		if _, visited := index[n]; !visited {
			visit(n)
		}
	}
	return components
}

func TestInternalSubtreesFormNoDependencyCycle(t *testing.T) {
	graph := subtreeGraph(t)
	if len(graph) == 0 {
		t.Fatal("no imports found under api/internal — the walk is broken, not the architecture clean")
	}
	for _, component := range stronglyConnected(graph) {
		inside := map[string]bool{}
		for _, n := range component {
			inside[n] = true
		}
		var edges []string
		for _, from := range component {
			for to, site := range graph[from] {
				if inside[to] {
					edges = append(edges, from+" -> "+to+"  ("+site+")")
				}
			}
		}
		sort.Strings(edges)
		t.Errorf("api/internal subtrees %v depend on each other in a cycle; edges inside it:\n  %s",
			component, strings.Join(edges, "\n  "))
	}
}

func TestStronglyConnectedFindsATwoNodeCycle(t *testing.T) {
	graph := map[string]map[string]string{
		"a": {"b": "a.go:1"},
		"b": {"a": "b.go:1"},
		"c": {"a": "c.go:1"},
	}
	got := stronglyConnected(graph)
	if len(got) != 1 || strings.Join(got[0], ",") != "a,b" {
		t.Fatalf("stronglyConnected = %v, want [[a b]]", got)
	}
}

func TestStronglyConnectedIgnoresAcyclicGraph(t *testing.T) {
	graph := map[string]map[string]string{
		"a": {"b": "a.go:1"},
		"b": {"c": "b.go:1"},
	}
	if got := stronglyConnected(graph); len(got) != 0 {
		t.Fatalf("stronglyConnected = %v, want none", got)
	}
}
