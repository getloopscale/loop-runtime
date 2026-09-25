// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routeauthcoverage

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
)

// # Why the enumeration is AST-based, and why the scope stopped being one file
//
// The previous enumerator was a line regex: `^\s*r\.(?:Get|Post|…)\(`. It requires
// `r.` to be followed IMMEDIATELY by the verb — and the most numerous shape in the
// real router is `r.With(rateLimitMiddlewares...).Post(…)`, where `With` comes next.
//
// Measured 2026-08-17: 42 direct registrations, **28** in the chained shape, 1 mounted
// by a helper in another file. The gate reported `42 routes` over ~71 and sold the
// guarantee "every public route is behind Auth" while checking 59% of them.
//
// None of the 29 invisible ones was unsafe — but by POSITION, not by verification:
// 27 fall inside `registerProtectedRoutes` and one is in the allowlist. Line 202 of the
// router (`r.With(middleware.RequireAdminService).Post(`) proves the blind shape is
// also used OUTSIDE the protected ranges, so the next one in that shape could be born
// open with the gate saying PASS.
//
// The AST sees the CALL, not the spelling: `r.With(a,b).Post(x)` and `r.Post(x)` are
// the same `*ast.CallExpr` with `Sel.Name == "Post"`, and the receiver is irrelevant.
//
// See MEET-ASYNC-AMENDMENT-2026-08-17-route-auth-coverage-is-blind-to-40-percent.

// ErrRouterUnparseable means router.go exists but is not valid Go.
//
// Distinct from `ErrRouterNotFound` on purpose: "I did not find the file" and "I found
// it and did not understand it" are different instrumentation errors, and collapsing
// them would make the gate enumerate zero and report the wrong error.
var ErrRouterUnparseable = errors.New("router.go exists but is not valid Go — the gate enumerated nothing, and that is an instrumentation error, not an absence of routes")

// ErrUndeclaredOffRouterMount means a production file outside router.go registers a
// route without a declared reason.
var ErrUndeclaredOffRouterMount = errors.New("a route is registered outside router.go without a declaration — the gate does not decide whether it is protected, it refuses not to see it")

// ErrEnumerationBelowFloor means the enumeration collapsed below the declared floor.
var ErrEnumerationBelowFloor = errors.New("the enumeration fell below the declared floor — a drop by half is the realistic failure mode, and refusing only zero would never catch it")

// routeVerbs are the chi registration methods. `Handle`/`HandleFunc`/`Method`/
// `MethodFunc` take the pattern in a later argument, which is why the pattern is
// located by SHAPE (first string literal starting with "/") and not by position.
var routeVerbs = map[string]bool{
	"Get": true, "Post": true, "Put": true, "Patch": true, "Delete": true,
	"Head": true, "Options": true, "Connect": true, "Trace": true,
	"Handle": true, "HandleFunc": true, "Method": true, "MethodFunc": true,
}

// RouteSite is one enumerated registration.
type RouteSite struct {
	File    string
	Line    int
	Verb    string
	Pattern string
}

// enumerateFile returns every route registration in one parsed file.
func enumerateFile(fset *token.FileSet, file *ast.File, rel string) []RouteSite {
	return enumerateNode(fset, file, rel)
}

// enumerateNode returns every route registration under one AST node.
func enumerateNode(fset *token.FileSet, node ast.Node, rel string) []RouteSite {
	var sites []RouteSite
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !routeVerbs[sel.Sel.Name] {
			return true
		}
		pattern, ok := firstRouteLiteral(call.Args)
		if !ok {
			// A verb without a literal pattern is not enumerable — it may be a variable.
			// We do not invent the value: moving on and letting the floor and the tree
			// scan cover the hole is more honest than guessing.
			return true
		}
		sites = append(sites, RouteSite{
			File:    rel,
			Line:    fset.Position(call.Lparen).Line,
			Verb:    sel.Sel.Name,
			Pattern: pattern,
		})
		return true
	})
	return sites
}

// firstRouteLiteral finds the first string argument that looks like a route path.
//
// Located by shape rather than by index because the chi verbs disagree on position:
// `Get(pattern, h)` puts it first, `Method(verb, pattern, h)` second. Indexing per
// verb would be a table that rots the day chi adds one.
func firstRouteLiteral(args []ast.Expr) (string, bool) {
	for _, a := range args {
		lit, ok := a.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			continue
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil || !strings.HasPrefix(s, "/") {
			continue
		}
		return s, true
	}
	return "", false
}

// authRanges locates, by AST position, the `r.Group(func(r chi.Router){…})` whose
// body references middleware.Auth, plus the body of registerProtectedRoutes.
//
// Positions come from the AST rather than from brace counting: a brace inside a
// string or a comment shifts a counter and silently moves the range, which is the
// class of defect this whole change removes.
func authRanges(fset *token.FileSet, file *ast.File) (authStart, authEnd, protStart, protEnd int, authCallsProtected bool) {
	qualifiers := authQualifiers(file)
	ast.Inspect(file, func(n ast.Node) bool {
		if fn, ok := n.(*ast.FuncDecl); ok && fn.Name.Name == "registerProtectedRoutes" && fn.Body != nil {
			protStart = fset.Position(fn.Body.Lbrace).Line
			protEnd = fset.Position(fn.Body.Rbrace).Line
			return true
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Group" || len(call.Args) == 0 {
			return true
		}
		lit, ok := call.Args[0].(*ast.FuncLit)
		if !ok || lit.Body == nil || !mentionsAuth(lit.Body, qualifiers) {
			return true
		}
		authStart = fset.Position(lit.Body.Lbrace).Line
		authEnd = fset.Position(lit.Body.Rbrace).Line
		authCallsProtected = calls(lit.Body, protectedEntry)
		return true
	})
	return
}

// authPackagePaths are the import paths that own the `Auth` middleware: the
// concern package it lives in today, and the flat package it lived in before
// B-059 split that one into five. Both are listed so this gate reads a tree
// from either side of that split.
var authPackagePaths = map[string]bool{
	"github.com/getloopscale/loopzero/api/internal/middleware/auth": true,
	"github.com/getloopscale/loopzero/api/internal/middleware":      true,
}

// authQualifiers returns the local names under which router.go imports the
// package that owns `Auth` — the alias when it has one, the path's last segment
// otherwise.
//
// Resolving the name from the import rather than hard-coding it is the
// difference between a gate that survives a rename and one that goes blind on
// it. Measured: when `internal/middleware` split into concern packages,
// router.go had to import the auth one as `mwauth`, because
// `internal/routes/auth` already owns the plain name — and a gate matching the
// letters `middleware.Auth` reported "could not locate the auth group" instead
// of the violations it exists to report. Failing to find its own subject is the
// one way an invariant gate can fail open.
func authQualifiers(file *ast.File) map[string]bool {
	names := map[string]bool{}
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || !authPackagePaths[path] {
			continue
		}
		if imp.Name != nil {
			names[imp.Name.Name] = true
			continue
		}
		names[path[strings.LastIndex(path, "/")+1:]] = true
	}
	return names
}

// mentionsAuth reports whether a block references the auth middleware's `Auth`
// — the proof that a chi Group is THE auth group and not some other grouping.
// `qualifiers` comes from authQualifiers, so an unrelated package that happens
// to export an `Auth` symbol does not qualify.
func mentionsAuth(block *ast.BlockStmt, qualifiers map[string]bool) bool {
	found := false
	ast.Inspect(block, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Auth" {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && qualifiers[id.Name] {
			found = true
		}
		return true
	})
	return found
}

// scanOffRouter walks the production tree for route registrations outside router.go.
//
// It deliberately does NOT try to decide whether such a route is protected. Doing so
// would mean following the call chain from the mount helper back to its caller, and
// an analysis that gets THAT wrong is worse than no analysis — it would vouch for a
// route it never understood. What it refuses is the invisibility.
func scanOffRouter(root string, declared map[string]string, protected []funcSpan) ([]string, error) {
	apiDir := filepath.Join(root, "api")
	var undeclared []string

	err := filepath.WalkDir(apiDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // a tree without api/ is not this gate's defect
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil //nolint:nilerr
		}
		rel = filepath.ToSlash(rel)
		if rel == routerPath {
			return nil
		}

		fset := token.NewFileSet()
		f, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			// A production file that does not parse is its author's problem, not this
			// gate's; ignoring it here keeps the gate from becoming the compile-error
			// reporter for the whole module.
			return nil //nolint:nilerr
		}
		var sites []RouteSite
		for _, s := range enumerateOnChiRouterParams(fset, f, rel) {
			// Registrations reached from registerProtectedRoutes are counted,
			// with their protection, by Check — not off-router mounts.
			if !inSpans(protected, s.File, s.Line) {
				sites = append(sites, s)
			}
		}
		if len(sites) == 0 {
			return nil
		}
		// Declared means declared WITH A REASON. An empty entry declares nothing — it
		// only silences, and silencing through an empty key is how an exemption stops
		// being useful.
		if reason, ok := declared[rel]; ok && strings.TrimSpace(reason) != "" {
			return nil
		}
		for _, s := range sites {
			undeclared = append(undeclared, fmt.Sprintf("%s:%d  %s %s  (registration outside router.go, no declaration in OffRouterMounts)",
				s.File, s.Line, s.Verb, s.Pattern))
		}
		return nil
	})
	return undeclared, err
}

// enumerateOnChiRouterParams counts only registrations made on a `chi.Router`
// RECEIVED AS A PARAMETER — that is, on the public router, mounted from outside.
//
// # Why the distinction is semantic and not a list of paths
//
// The first version of this scan counted any HTTP registration under `api/`, and
// found 10 sites. Nine of them have nothing to do with the router this gate audits:
// `/debug/pprof/*` on a dedicated `http.ServeMux` and its own port
// (`api/cmd/main.go`), `/healthz`+`/readyz`+`/metrics` on the mux of the
// **controller** binary, and `/metrics` on another mux (`internal/server/validation.go`).
//
// Excluding them by path would produce a nine-entry exception list — the mechanism
// the MEET's dissenter pointed to as the one that ages badly, and it would have to
// grow with every new mux. The property that actually separates them is another one,
// and it does not age: **a route registered on a `chi.Router` the function received
// is a mount point of the public router; a route on a local `ServeMux` is another
// server.**
//
// This is syntactic analysis, not type analysis: it matches `chi.Router` by the shape
// of the parameter expression. A local alias (`type R = chi.Router`) would escape —
// and it escapes to the SAFE side, because the registration is simply not counted as
// a mount on the public router, which is what it in fact is not until someone proves
// otherwise.
func enumerateOnChiRouterParams(fset *token.FileSet, file *ast.File, rel string) []RouteSite {
	var sites []RouteSite
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Type.Params == nil {
			continue
		}
		routers := map[string]bool{}
		for _, field := range fn.Type.Params.List {
			sel, ok := field.Type.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Router" {
				continue
			}
			if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "chi" {
				continue
			}
			for _, name := range field.Names {
				routers[name.Name] = true
			}
		}
		if len(routers) == 0 {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !routeVerbs[sel.Sel.Name] {
				return true
			}
			recv, ok := sel.X.(*ast.Ident)
			if !ok || !routers[recv.Name] {
				return true
			}
			pattern, ok := firstRouteLiteral(call.Args)
			if !ok {
				return true
			}
			sites = append(sites, RouteSite{File: rel, Line: fset.Position(call.Lparen).Line, Verb: sel.Sel.Name, Pattern: pattern})
			return true
		})
	}
	return sites
}
