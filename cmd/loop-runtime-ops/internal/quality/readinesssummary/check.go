// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package readinesssummary enforces the ADR-0025 D3 invariant across the SIX reconciler
// packages, not just one.
//
// The invariant is one of ORDER: the readiness summary is the LAST status mutation
// before persisting, even on an early return. Order is what a table that counts calls
// does not see — that is how B-105 stayed hidden.
//
// Why this gate exists when there was an older guard: the guard that lived in
// `operators/internal/app/exposure_status_writer_test.go` is PACKAGE-scoped and looks
// only at the function that ENCLOSES each `Patch`. The canonical shape of the five
// reconcilers is different — `defer reconcileloop.DeferReadinessSummary(...)()` in
// `Reconcile`, with the persists living in helpers it calls. To the old guard that is
// absence: that is how `preview` measured "0 of 6" while having coverage (B-138).
//
// `DeferReadinessSummary` re-fetches the CR and PERSISTS the summary on return, that is,
// it is the last write of the cycle. That is why coverage PROPAGATES through calls: a
// persist in a function reachable from the one carrying the defer is covered. The inline
// fold does not propagate — it is a mutation before the write of THAT function.
//
// MEASURED LIMITS, and they are part of the result. The graph is intra-package and
// functions are indexed by RECEIVER+NAME, not by name: `operators/internal/app` has TWO
// `Reconcile` (Application and Exposure) and only Exposure carries the defer — indexing
// by name made one's defer cover the other's helpers, and the gate reported green over
// an uncovered persist. Call resolution is conservative: `r.foo()` on the receiver
// resolves to `T.foo`, `foo()` resolves to the free function, and ANY other form stays
// unresolved and does not propagate. Erring toward not-propagating produces a false
// positive, which somebody reads; the opposite produces a false green, which nobody reads.
//
// Coverage is also PATH-INSENSITIVE: if a helper is reachable from the function that
// has the defer, it counts as covered even if another path reaches it without the
// defer. Measured on 2026-08-19, that is acceptable here and the reason is verifiable:
// the SEVEN defers in the tree are all in `Reconcile`, which is the only entry point
// controller-runtime calls. If a defer shows up outside a `Reconcile`, this premise
// no longer holds.
package readinesssummary

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// DefaultPackages are the six reconciler packages B-138 declares as the population.
var DefaultPackages = []string{
	filepath.Join("operators", "internal", "app"),
	filepath.Join("operators", "internal", "environment"),
	filepath.Join("operators", "internal", "platform"),
	filepath.Join("operators", "internal", "preview"),
	filepath.Join("operators", "internal", "reconcileloop"),
	filepath.Join("operators", "internal", "tenant"),
}

// Config drives the gate.
type Config struct {
	RepoRoot string
	Packages []string
}

// Site is a `.status` persist and the function that contains it.
type Site struct {
	File  string
	Line  int
	Func  string
	Shape string
}

// Outcome carries the gate result.
type Outcome struct {
	Pass       bool
	Packages   int
	Sites      int
	Violations []Site
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

type function struct {
	name      string
	inline    bool
	withDefer bool
	calls     map[string]bool
	sites     []Site
}

// Check runs the gate.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	packages := cfg.Packages
	if len(packages) == 0 {
		packages = DefaultPackages
	}

	for _, rel := range packages {
		dir := filepath.Join(cfg.RepoRoot, rel)
		entries, err := os.ReadDir(dir)
		if err != nil {
			// A missing package is a FAILURE, never a skip: a scan that reaches
			// nothing reports green over nothing.
			return out, fmt.Errorf("reading %s: %w", rel, err)
		}
		out.Packages++

		funcs := map[string]*function{}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			if err := scan(cfg.RepoRoot, filepath.Join(dir, name), funcs); err != nil {
				return out, err
			}
		}
		collect(funcs, &out)
	}

	sort.Slice(out.Violations, func(i, j int) bool {
		if out.Violations[i].File != out.Violations[j].File {
			return out.Violations[i].File < out.Violations[j].File
		}
		return out.Violations[i].Line < out.Violations[j].Line
	})
	out.Pass = len(out.Violations) == 0
	return out, nil
}

func scan(root, path string, funcs map[string]*function) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	rel, _ := filepath.Rel(root, path)

	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		recv, ident := receiver(fd)
		key := fd.Name.Name
		if recv != "" {
			key = recv + "." + fd.Name.Name
		}
		fn := funcs[key]
		if fn == nil {
			fn = &function{name: key, calls: map[string]bool{}}
			funcs[key] = fn
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.DeferStmt:
				if calledName(v.Call) == "DeferReadinessSummary" || callsInside(v.Call, "DeferReadinessSummary") {
					fn.withDefer = true
				}
			case *ast.CallExpr:
				target := calledName(v)
				if k := resolve(v, recv, ident); k != "" {
					fn.calls[k] = true
				}
				if target == "SetReadinessSummaryViaPkg" || target == "SetReadinessSummary" {
					fn.inline = true
				}
				if shape := persistShape(v); shape != "" {
					fn.sites = append(fn.sites, Site{
						File: rel, Line: fset.Position(v.Pos()).Line,
						Func: key, Shape: shape,
					})
				}
			}
			return true
		})
	}
	return nil
}

// collect propagates the `defer` coverage through calls and gathers what is left.
func collect(funcs map[string]*function, out *Outcome) {
	covered := map[string]bool{}
	var queue []string
	for name, fn := range funcs {
		if fn.withDefer {
			covered[name] = true
			queue = append(queue, name)
		}
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		fn := funcs[current]
		if fn == nil {
			continue
		}
		for target := range fn.calls {
			if !covered[target] && funcs[target] != nil {
				covered[target] = true
				queue = append(queue, target)
			}
		}
	}
	for name, fn := range funcs {
		out.Sites += len(fn.sites)
		if fn.inline || covered[name] {
			continue
		}
		out.Violations = append(out.Violations, fn.sites...)
	}
}

// persistShape returns "Patch"/"Update" when the call is `...Status().X(...)`.
func persistShape(c *ast.CallExpr) string {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if sel.Sel.Name != "Patch" && sel.Sel.Name != "Update" {
		return ""
	}
	if !callsInsideExpr(sel.X, "Status") {
		return ""
	}
	return sel.Sel.Name
}

// receiver returns the name of the receiver's TYPE and the identifier used for it.
func receiver(fd *ast.FuncDecl) (typeName, ident string) {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return "", ""
	}
	field := fd.Recv.List[0]
	if len(field.Names) > 0 {
		ident = field.Names[0].Name
	}
	e := field.Type
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	if id, ok := e.(*ast.Ident); ok {
		typeName = id.Name
	}
	return typeName, ident
}

// resolve returns the key of the called function, or "" when it cannot be resolved.
// Conservative on purpose: not resolving does not propagate coverage.
func resolve(c *ast.CallExpr, recvType, recvIdent string) string {
	switch f := c.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		if id, ok := f.X.(*ast.Ident); ok && recvIdent != "" && id.Name == recvIdent {
			return recvType + "." + f.Sel.Name
		}
	}
	return ""
}

func calledName(c *ast.CallExpr) string {
	switch f := c.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	case *ast.CallExpr: // `Helper(...)()`
		return calledName(f)
	}
	return ""
}

func callsInside(c *ast.CallExpr, name string) bool { return callsInsideExpr(c, name) }

func callsInsideExpr(e ast.Expr, name string) bool {
	var found bool
	ast.Inspect(e, func(n ast.Node) bool {
		if ce, ok := n.(*ast.CallExpr); ok && calledName(ce) == name {
			found = true
			return false
		}
		return true
	})
	return found
}

// Summary describes the outcome.
func (o Outcome) Summary() string {
	return fmt.Sprintf("%d package(s), %d .status persist(s), %d without the summary fold",
		o.Packages, o.Sites, len(o.Violations))
}
