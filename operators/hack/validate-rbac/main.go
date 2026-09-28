// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package main implements the validate-rbac AST analyzer per ADR-0026 D4
// + plan T4.3.
//
// What it checks, and only this:
//
//  1. Default mode — every +kubebuilder:rbac marker parses and declares
//     verbs, and none uses the banned `resources=*` / `verbs=*` wildcards.
//
//  2. --strict — "marker without call": a marker none of whose verbs is
//     used by an r.<Verb>(...) call in the same file. A verb-level
//     heuristic: without type information the (group, resource) half of
//     the marker is not matched.
//
// What it does NOT check: a call with no marker covering it. Deciding which
// (group, resource) an r.Get(ctx, key, obj) touches needs the type of obj,
// which this AST pass does not resolve, so a missing permission still
// surfaces only at runtime as Forbidden. KNOWN_GAPS.md § 0 records this.
//
// Per ADR-0026 condition #5 (security-engineer): style-guide rules
// enforced inline — bans `resources=*` and `verbs=*` wildcards.
//
// Known blind spots (documented in KNOWN_GAPS.md):
//   - Dynamic client (`dynamic.NewForConfig(...).Resource(gvr).Get`)
//   - Helper indirection (`shared.applyServerSide(ctx, obj)`)
//   - Cross-cluster clients (r.RuntimeClient, r.BuildClient — covered
//     by separate `kubectl auth can-i` matrix per ADR-0026 #2)
//   - Watches/informers requiring implicit list+watch
//   - EventRecorder calls
//
// Exit codes:
//
//	0 — all checks pass
//	1 — analyzer detected violation(s)
//	2 — usage error / file not found
//
// Usage:
//
//	go run ./hack/validate-rbac -dir operators/internal/tenant
//	task validate:rbac:ast  # CI gate
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// markerRule represents a single +kubebuilder:rbac directive parsed from
// a struct doc comment.
type markerRule struct {
	groups    []string
	resources []string
	verbs     []string
	pos       token.Position // for error reporting
}

// callSite is a captured r.Client.X(...) invocation.
type callSite struct {
	verb string // Get, List, Create, Update, Patch, Delete
	pos  token.Position
}

// errExit is the analyzer's structured error reporter; stays terse so CI
// log output is grep-friendly.
type errExit struct {
	violations []string
}

func (e *errExit) add(format string, args ...interface{}) {
	e.violations = append(e.violations, fmt.Sprintf(format, args...))
}

func main() {
	dir := flag.String("dir", "operators/internal", "directory to analyze (recursive)")
	strict := flag.Bool("strict", false, "fail on dead markers (markers without matching call)")
	flag.Parse()

	exit := &errExit{}
	fset := token.NewFileSet()

	if err := filepath.Walk(*dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		return analyzeFile(fset, path, exit, *strict)
	}); err != nil {
		fmt.Fprintf(os.Stderr, "walk: %v\n", err)
		os.Exit(2)
	}

	if len(exit.violations) > 0 {
		fmt.Fprintln(os.Stderr, "validate-rbac: violations detected:")
		for _, v := range exit.violations {
			fmt.Fprintln(os.Stderr, "  - "+v)
		}
		fmt.Fprintf(os.Stderr, "\n%d violation(s) (per ADR-0026 D4)\n", len(exit.violations))
		os.Exit(1)
	}
	fmt.Println("validate-rbac: OK")
}

func analyzeFile(fset *token.FileSet, path string, exit *errExit, strict bool) error {
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	// Pass 1 — collect markers (per struct doc-comment).
	markers := []markerRule{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE || gd.Doc == nil {
			continue
		}
		for _, c := range gd.Doc.List {
			line := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "+kubebuilder:rbac:") {
				continue
			}
			rule, perr := parseRBACMarker(line, fset.Position(c.Pos()))
			if perr != nil {
				exit.add("%s: %v", fset.Position(c.Pos()), perr)
				continue
			}
			// Style-guide enforcement (ADR-0026 condition #5).
			for _, r := range rule.resources {
				if r == "*" {
					exit.add("%s: marker uses banned wildcard `resources=*` — list resources explicitly", rule.pos)
				}
			}
			for _, v := range rule.verbs {
				if v == "*" {
					exit.add("%s: marker uses banned wildcard `verbs=*` — enumerate verbs explicitly", rule.pos)
				}
			}
			markers = append(markers, rule)
		}
	}

	// Pass 2 — collect r.Client.X call sites.
	calls := []callSite{}
	ast.Inspect(f, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := ce.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// Match r.<verb>(...) directly (TenantReconciler embeds client.Client).
		// r.Client.<verb>(...) and r.Status() are not flagged here; status
		// writes are governed by the single-status-writer invariant
		// (CLAUDE.md; ADR 2026-08-12-status-writer-contract), not by RBAC.
		ident, ok := sel.X.(*ast.Ident)
		if !ok || ident.Name != "r" {
			return true
		}
		switch sel.Sel.Name {
		case "Get", "List", "Create", "Update", "Patch", "Delete":
			calls = append(calls, callSite{
				verb: sel.Sel.Name,
				pos:  fset.Position(ce.Pos()),
			})
		}
		return true
	})

	// Call sites are only used by --strict: nothing here maps a call to the
	// (group, resource) it touches, so a call without a marker is not
	// reported (see the package doc).
	if strict {
		// Mark each marker as "unused" by default; clear flag if we find
		// any matching call. Simplistic: we only verify the verb appears
		// somewhere in the file's call set, not the (group, resource)
		// triple. Production analyzer would need full type info.
		for _, rule := range markers {
			if len(rule.verbs) == 0 {
				continue
			}
			hit := false
			for _, c := range calls {
				if verbMatch(c.verb, rule.verbs) {
					hit = true
					break
				}
			}
			if !hit {
				exit.add("%s: marker for verbs=%v has NO matching r.Client.X call in this file (dead permission?)",
					rule.pos, rule.verbs)
			}
		}
	}

	return nil
}

// parseRBACMarker parses one +kubebuilder:rbac:groups=...,resources=...,verbs=...
// directive into structured form. Positional minimum check; controller-gen
// itself does the full validation at generate time.
func parseRBACMarker(line string, pos token.Position) (markerRule, error) {
	rule := markerRule{pos: pos}
	// Strip prefix.
	body := strings.TrimPrefix(line, "+kubebuilder:rbac:")
	// Each segment is key=val,val,val separated by comma BETWEEN keys.
	// We split on commas; rejoin chunks that come after `verbs=` etc.
	// Simpler: regex-style split on `<key>=<val>` boundaries.
	parts := strings.Split(body, ",")
	currentKey := ""
	for _, p := range parts {
		if eq := strings.Index(p, "="); eq >= 0 {
			currentKey = p[:eq]
			rest := p[eq+1:]
			rest = strings.Trim(rest, `"`)
			switch currentKey {
			case "groups":
				rule.groups = append(rule.groups, rest)
			case "resources":
				rule.resources = append(rule.resources, rest)
			case "verbs":
				rule.verbs = append(rule.verbs, rest)
			}
		} else {
			// Continuation chunk (comma-separated value within a key).
			val := strings.Trim(p, `"`)
			switch currentKey {
			case "groups":
				rule.groups = append(rule.groups, val)
			case "resources":
				rule.resources = append(rule.resources, val)
			case "verbs":
				rule.verbs = append(rule.verbs, val)
			}
		}
	}
	if len(rule.verbs) == 0 {
		return rule, fmt.Errorf("marker missing verbs= clause")
	}
	return rule, nil
}

// verbMatch returns true if call (e.g., "Get") corresponds to any of
// the marker's verbs (which use lowercase per K8s convention).
func verbMatch(callVerb string, markerVerbs []string) bool {
	c := strings.ToLower(callVerb)
	for _, raw := range markerVerbs {
		// markerVerbs may contain semicolon-joined lists.
		for _, mv := range strings.Split(raw, ";") {
			if strings.TrimSpace(mv) == c {
				return true
			}
		}
	}
	return false
}
