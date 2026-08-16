// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package silentsurface refuses NEW HTTP surfaces that disappear without saying so.
//
// # What it measures
//
// `api/internal/server/router.go` decides, per surface, whether to register it:
// `if cfg.X != nil { r.Post(...) }`. When the dependency is absent the route
// simply is not there. Measured on the droplet on 2026-08-14: with
// `GITHUB_WEBHOOK_SECRET` unset, `POST /api/v1/webhooks/github` answered 404
// while `/api/v1/health` answered 405 — and nothing anywhere reported it. In
// production that means a customer's `git push` builds nothing, silently (B-104).
//
// The router had FIFTY such guards. Recording all fifty in one commit was
// refused deliberately: `router.go` is the single surface where the route set is
// read (Unbreakable Rule 5), and an error in any one of fifty edits would be
// indistinguishable from the other forty-nine.
//
// # Why a monotonic ratchet
//
// So this gate does what `unhomed-logic` does for shell scripts: it COUNTS the
// debt and forbids it from growing. The baseline may fall — that is real homing —
// and may never rise. Surface fifty-one cannot be born mute.
//
// This is the inversion `status-writer-contract` established in the same session:
// a per-instance test protects the instance somebody remembered; a gate that
// walks the whole file fails on the ABSENCE of protection.
package silentsurface

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	gotypes "go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Baseline is the number of route-registering guards that do NOT report their
// decision, measured at the moment this gate landed.
//
// Measured 2026-08-16 against `api/internal/server/router.go`: **35** guards
// register a route, of which **34** do not report the decision (the GitHub
// webhook is the one that does — B-104's measured instance).
//
// The number is the MEASURED one and not the fifty that a grep for
// `if cfg.X != nil` returns. Fifteen of those fifty mount middleware or
// configure an already-registered handler; they register no surface, so counting
// them would set the baseline fifteen above the truth — and a ratchet with slack
// admits fifteen new mute surfaces before it says anything, which is the
// "guard that does not guard" this repository keeps paying for.
//
// It may only ever go DOWN: each decrement is one surface that became
// observable. Raising it is what this gate exists to refuse.
//
// # Ledger
//
//	34  2026-08-16  B-104 landed: the GitHub webhook reports; 34 do not.
//	26  2026-08-16  B-115 slice 1: the platform's own control paths — bootstrap,
//	                argocd-callback, secret-fetch, admin evict/pin, release
//	                inspect, the authenticated group, and active-team. Converted
//	                first because their absence is invisible from the product
//	                side: nobody files a ticket when the ESO broker stops
//	                answering, the fleet just stops converging.
//	13  2026-08-16  B-115 slice 2: the product data plane — upload, projects,
//	                events, secrets, the build family, logs, status, metrics
//	                proxy and scale. Measured: every one is built inside a chain
//	                that begins at `if dbAdapter != nil` (main.go:167), so the
//	                whole group shares one root cause and their reasons name the
//	                chain rather than the rung.
const Baseline = 13

// RouterPath is the file the gate reads, relative to the repository root.
const RouterPath = "api/internal/server/router.go"

// Site is one route-registering guard that does not report its decision.
type Site struct {
	// Field is the Config field the guard tests, e.g. "AdminEvictor".
	Field string
	// Line is where the guard opens.
	Line int
}

// Outcome is what one analysis measured.
type Outcome struct {
	// Guarded is every `if cfg.X != nil` that registers at least one route.
	Guarded int
	// Silent are the guarded surfaces with no Record call.
	Silent []Site
}

// Verdict is an Outcome judged against a baseline.
type Verdict struct {
	Outcome
	Pass     bool
	Baseline int
}

// Summary renders the one-line result the gate runner prints.
func (v Verdict) Summary() string {
	if v.Pass {
		return fmt.Sprintf("PASS — %d superfície(s) muda(s) sobre uma base de %d "+
			"(%d guarda(s) de rota no total)", len(v.Silent), v.Baseline, v.Guarded)
	}
	var names []string
	for _, s := range v.Silent {
		names = append(names, fmt.Sprintf("%s:%d", s.Field, s.Line))
	}
	if len(names) > 8 {
		names = append(names[:8], "…")
	}
	return fmt.Sprintf("FAIL — %d superfície(s) muda(s), acima da base de %d. "+
		"Uma superfície nova nasceu sem reportar que pode estar desligada: %s",
		len(v.Silent), v.Baseline, strings.Join(names, ", "))
}

// ActionableFix is printed under a failure.
const ActionableFix = `Grave a decisão que o roteador já toma:

    if cfg.X != nil {
        r.Post("/…", cfg.X.ServeHTTP)
        _ = cfg.Surfaces.Record(surfaces.X, true, "")
    } else {
        _ = cfg.Surfaces.Record(surfaces.X, false, "QUAL_SEGREDO ausente — o que ele serve")
    }

Sem isso a superfície some em silêncio quando a dependência falta, e o único
vestígio é um 404 que alguém descobre tarde. Contrato: B-104.`

// registrarMethods are the chi calls that put a route on the mux. `Use` is
// deliberately absent: mounting middleware is not registering a surface, and
// counting it would inflate the baseline with work that does not exist.
var registrarMethods = map[string]bool{
	"Get": true, "Post": true, "Put": true, "Patch": true, "Delete": true,
	"Head": true, "Options": true, "Handle": true, "HandleFunc": true,
	"Mount": true, "Route": true, "Method": true,
}

// guardedField returns the Config field a `cfg.X != nil` condition tests, or "".
// A compound condition (`a != nil && b != nil`) reports the first field, which is
// enough: the fix records once for the surface either way.
func guardedField(cond ast.Expr) string {
	switch c := cond.(type) {
	case *ast.CallExpr:
		// The `Recorded` form wraps the test in a call:
		//   if surfaces.Recorded(reg, name, cfg.X != nil, "…") { … }
		// The field lives in an argument, so recurse into them. Recognising
		// only the bare `cfg.X != nil` shape would make the gate blind to the
		// very form it asks authors to adopt — it would count a reporting
		// surface as no surface at all, and the baseline would drift down for
		// the wrong reason.
		for _, arg := range c.Args {
			if f := guardedField(arg); f != "" {
				return f
			}
		}
		return ""
	case *ast.BinaryExpr:
		if c.Op == token.LAND {
			if f := guardedField(c.X); f != "" {
				return f
			}
			return guardedField(c.Y)
		}
		if c.Op != token.NEQ {
			return ""
		}
		if id, ok := c.Y.(*ast.Ident); !ok || id.Name != "nil" {
			return ""
		}
		sel, ok := c.X.(*ast.SelectorExpr)
		if !ok {
			return ""
		}
		if recv, ok := sel.X.(*ast.Ident); !ok || recv.Name != "cfg" {
			return ""
		}
		return sel.Sel.Name
	}
	return ""
}

// containsRouteRegistration reports whether a block puts a route on the mux.
func containsRouteRegistration(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !registrarMethods[sel.Sel.Name] {
			return true
		}
		// `r.Post(...)` / `r.With(...).Post(...)` — the receiver chain has to
		// start at the router, otherwise `store.Get(...)` would count.
		if strings.HasPrefix(gotypes.ExprString(sel.X), "r") {
			found = true
			return false
		}
		return true
	})
	return found
}

// recorderNames are the two spellings that report a registration decision.
//
// `Record` is the registry method — the explicit if/else form. `Recorded` is the
// helper that records AND returns the decision, so the guard's own condition is
// the report:
//
//	if surfaces.Recorded(cfg.Surfaces, surfaces.Bootstrap, cfg.X != nil, "…") {
//
// The gate has to know both. A fence that only recognises the spelling its
// author reached for reports clean because it is looking away — the same lesson
// `status-writer-contract` learned when a two-line alias evaded it.
var recorderNames = map[string]bool{"Record": true, "Recorded": true}

// containsRecord reports whether a node reports a registration decision.
//
// The whole if-statement is inspected, including its CONDITION, because the
// `Recorded` form puts the report there rather than in the body.
func containsRecord(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && recorderNames[sel.Sel.Name] {
			found = true
			return false
		}
		return true
	})
	return found
}

// Analyze measures one router source.
//
// It takes the source text rather than a path so the behaviour can be pinned with
// fixtures instead of against the real router, whose shape changes for reasons
// that have nothing to do with this contract.
func Analyze(src string) (Outcome, error) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "router.go", src, 0)
	if err != nil {
		return Outcome{}, fmt.Errorf("parsing router source: %w", err)
	}

	var out Outcome
	ast.Inspect(parsed, func(n ast.Node) bool {
		ifStmt, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		field := guardedField(ifStmt.Cond)
		if field == "" || !containsRouteRegistration(ifStmt.Body) {
			return true
		}
		out.Guarded++
		// The record may sit in the body, in the else, or in both. Judging the
		// whole statement is what allows the canonical shape — enabled in the
		// body, disabled in the else — to count once.
		if !containsRecord(ifStmt) {
			out.Silent = append(out.Silent, Site{
				Field: field, Line: fset.Position(ifStmt.Pos()).Line,
			})
		}
		return true
	})

	sort.Slice(out.Silent, func(i, j int) bool { return out.Silent[i].Line < out.Silent[j].Line })
	return out, nil
}

// Evaluate judges an Outcome against a baseline. Fewer is always a pass.
func Evaluate(o Outcome, baseline int) Verdict {
	return Verdict{Outcome: o, Baseline: baseline, Pass: len(o.Silent) <= baseline}
}

// Config parameterises one run.
type Config struct {
	// Root is the repository root.
	Root string
}

// Check reads the real router and judges it against Baseline.
func Check(cfg Config) (Verdict, error) {
	path := filepath.Join(cfg.Root, RouterPath)
	src, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return Verdict{}, fmt.Errorf("reading %s: %w", RouterPath, err)
	}
	out, err := Analyze(string(src))
	if err != nil {
		return Verdict{}, err
	}
	// Measuring zero guards against the REAL router means the file moved, was
	// restructured, or the shapes this gate recognises stopped existing.
	// "Nothing to check" and "everything is clean" are different facts, and
	// reporting the second for the first is how a gate goes quiet without anyone
	// noticing. The check lives here and not in Analyze because a fixture with
	// zero guards is legitimate — that is what Analyze is for.
	if out.Guarded == 0 {
		return Verdict{}, fmt.Errorf("%s has no `if cfg.X != nil` guard registering a route — "+
			"either the router was restructured or this gate stopped recognising it; "+
			"a gate that measures nothing verifies nothing", RouterPath)
	}
	return Evaluate(out, Baseline), nil
}
