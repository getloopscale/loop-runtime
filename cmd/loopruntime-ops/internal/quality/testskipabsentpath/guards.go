// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package testskipabsentpath

import (
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/skipshape"
)

// siteWithPos carries the token position until the FileSet turns it into a line,
// plus whether the guard came from the caller of a skip-carrying helper.
type siteWithPos struct {
	Site
	pos        token.Pos
	fromCaller bool
}

// maxDelegatedSkipDepth bounds how far a skip is followed through helpers that
// CARRY it -- `skipUnless(t, cliPresent(), "...")` being one hop. It is a
// separate bound from maxResolveDepth because it counts call frames entered, not
// expression hops followed, and entering a frame costs a whole body walk.
const maxDelegatedSkipDepth = 8

// guardedSkips walks one function and returns each skip whose control depends
// on a filesystem probe.
//
// WHERE THE PROBE IS. Four shapes occur and all are handled: the probe inline in
// the if's init (`if _, err := os.Stat(p); os.IsNotExist(err) { t.Skip() }`), the
// probe on a preceding statement (`data, err := os.ReadFile(p)` followed by
// `if err != nil { t.Skip() }`), the probe down inside a same-package predicate
// the guard calls (`if !cliSrcPresent() { t.Skip() }`), and that same predicate
// bound to a local first (`ok, _ := cliSrcPresent()` then `if !ok { ... }`),
// which is the only way a predicate returning `(bool, error)` can be spelled.
//
// WHERE THE SKIP IS is the other half, and it is the half three audits kept
// walking through (B-188). A guard is read wherever the statement walk reaches:
// inside a function literal (B-186), inside a `defer`/`go`, inside a `select` or
// type-switch clause, in the CONDITION of a tag-less `switch` case, and inside a
// same-package helper the test hands its predicate to. Every one of those was a
// one-line refactor away from a guard this walker could not see.
//
// `seen` holds the names of the bodies already entered on this path, so a helper
// that calls back into one ends the walk instead of running it forever.
func guardedSkips(fd *ast.FuncDecl, r *resolver, seen map[string]bool, depth int) []siteWithPos {
	var sites []siteWithPos
	if fd.Body == nil {
		return nil
	}
	probes := map[string]ast.Expr{} // error variable -> the path it probed

	var walk func(stmts []ast.Stmt)

	// enterLits walks every function literal written inside an expression. The
	// probe map is shared with the enclosing body on purpose: a probe taken
	// outside the closure and tested inside it is one guard, and scoping them
	// apart would lose it.
	// A literal's body is walked UNDER CONDITION: whoever received it decides
	// whether it runs, and `defer func(){ f = other }()` and
	// `t.Cleanup(func(){ f = other })` both run after the guard, not before it.
	// Reading such an `=` as the name's live value is how both walked past in
	// silence (B-193).
	enterLits := func(e ast.Expr) {
		if e == nil {
			return
		}
		ast.Inspect(e, func(n ast.Node) bool {
			if lit, ok := n.(*ast.FuncLit); ok && lit.Body != nil {
				r.underCondition(func() { walk(lit.Body.List) })
			}
			return true
		})
	}
	// enterHelper follows a call to a same-package helper that CARRIES a skip,
	// reading the helper's body with the caller's arguments bound. The site is
	// reported at the CALL, which is the line a reader has to change.
	enterHelper := func(e ast.Expr, local map[string]ast.Expr) {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return
		}
		for _, s := range r.delegatedSkips(call, local, seen, depth) {
			s.pos = call.Pos()
			sites = append(sites, s)
		}
	}

	walk = func(stmts []ast.Stmt) {
		for _, st := range stmts {
			switch s := st.(type) {
			case *ast.AssignStmt:
				r.bindAssign(s)
				r.bindBoolPred(s)
				if name, pathExpr, ok := probeBinding(s); ok {
					probes[name] = pathExpr
				}
				for _, rhs := range s.Rhs {
					enterLits(rhs)
				}
			case *ast.DeclStmt:
				r.bindDecl(s)
			case *ast.IfStmt:
				local := probes
				if init, ok := s.Init.(*ast.AssignStmt); ok {
					r.bindAssign(init)
					r.bindBoolPred(init)
					if name, pathExpr, ok := probeBinding(init); ok {
						local = withProbe(probes, name, pathExpr)
					}
				}
				if tgt, arm, ok := r.skipArm(s, local); ok {
					sites = append(sites, r.newSite(tgt, skipPos(arm)))
				}
				// Both arms are walked under condition: at most one of them runs,
				// so an `=` in either is a value the name MAY hold, never the one
				// the statements after the if certainly read.
				r.underCondition(func() {
					if s.Body != nil {
						walk(s.Body.List)
					}
					if s.Else != nil {
						switch e := s.Else.(type) {
						case *ast.BlockStmt:
							walk(e.List)
						case *ast.IfStmt:
							walk([]ast.Stmt{e})
						}
					}
				})
			case *ast.BlockStmt:
				// A bare block always runs, so it does not raise the doubt.
				walk(s.List)
			case *ast.LabeledStmt:
				walk([]ast.Stmt{s.Stmt})
			case *ast.ForStmt:
				r.underCondition(func() {
					if s.Body != nil {
						walk(s.Body.List)
					}
				})
			case *ast.RangeStmt:
				// The binding is scoped to the body when the range DECLARES it.
				// Binding it into the frame permanently let it answer for the
				// outer name after the loop, and a guard reading that name went
				// from a correct verdict to SILENCE -- the class this gate exists
				// to close, introduced by a fix for it. Measured 2026-09-06.
				restore := r.bindRangeValue(s)
				r.underCondition(func() {
					if s.Body != nil {
						walk(s.Body.List)
					}
				})
				restore()
			case *ast.SwitchStmt:
				local := probes
				if init, ok := s.Init.(*ast.AssignStmt); ok {
					r.bindAssign(init)
					r.bindBoolPred(init)
					if name, pathExpr, ok := probeBinding(init); ok {
						local = withProbe(probes, name, pathExpr)
					}
				}
				sites = append(sites, r.switchSkips(s, local)...)
				r.underCondition(func() {
					if s.Body != nil {
						walk(s.Body.List)
					}
				})
			case *ast.TypeSwitchStmt:
				// `switch v := x.(type)` never carries a path condition, but its
				// clauses carry statements, and this walker used to match only
				// *ast.SwitchStmt -- so a guard in one was not read at all.
				r.underCondition(func() {
					if s.Body != nil {
						walk(s.Body.List)
					}
				})
			case *ast.SelectStmt:
				r.underCondition(func() {
					if s.Body != nil {
						walk(s.Body.List)
					}
				})
			case *ast.CaseClause:
				walk(s.Body)
			case *ast.CommClause:
				walk(s.Body)
			case *ast.ExprStmt:
				// `t.Run("name", func(t *testing.T){ ... })`, `func(){ ... }()`,
				// and `skipUnless(t, cliPresent(), "...")`.
				enterLits(s.X)
				enterHelper(s.X, probes)
			case *ast.DeferStmt:
				enterLits(s.Call)
				enterHelper(s.Call, probes)
			case *ast.GoStmt:
				enterLits(s.Call)
			}
		}
	}
	walk(fd.Body.List)
	return sites
}

// bindRangeValue records the VALUE variable of a `for _, f := range probes` as
// what it is: an entry of the ranged container, chosen by an index this gate
// cannot read.
//
// It is bound to a synthetic IndexExpr over the ranged expression so the whole
// question routes to the readers `probes[k]()` already goes to -- funcValue
// refuses it because the index is not a literal, and unreadableFuncValue turns
// that refusal into the UNRESOLVED verdict, over the same containerReachesProbe
// bar. Writing a second reader for the range spelling would be a second code
// path to keep in step with the first, and the gap between two such paths is
// what this item keeps reopening on: `probes[k]()` was UNRESOLVED and red while
// `for _, f := range probes { f() }` over THE SAME map was silent (B-193).
//
// A `for _, f = range probes` over a name the frame ALREADY bound is recorded
// as rebound on top of that: a range runs its body zero times over an empty
// container, so which of the two values a later guard reads is the same
// undecidable question, asked by the other door.
//
// It returns the UNDO, and which door the range used decides whether there is one.
// `for _, f := range xs` DECLARES a variable whose scope is the loop: after the
// loop Go reads whatever `f` held before, so the binding must not survive. Letting
// it survive is what turned a live guard silent -- `f := cliPresent; for _, f :=
// range ...; if !f()` reported the right path before the range spelling was taught
// at all, and nothing after it.
//
// `for _, f = range xs` assigns to a name the frame already holds, and THAT does
// outlive the loop: a range runs its body zero times over an empty container, so
// which of the two values a later guard reads is the undecidable question the
// rebound marking turns into UNRESOLVED. No undo for that one.
func (r *resolver) bindRangeValue(s *ast.RangeStmt) func() {
	noop := func() {}
	if s.Value == nil || s.X == nil {
		return noop
	}
	id, ok := s.Value.(*ast.Ident)
	if !ok || id.Name == "_" {
		return noop
	}
	prev, had := r.locals[id.Name]
	if had && s.Tok == token.ASSIGN {
		if r.reboundLocals == nil {
			r.reboundLocals = map[string]bool{}
		}
		r.reboundLocals[id.Name] = true
	}
	r.bind(id.Name, &ast.IndexExpr{X: s.X, Index: ast.NewIdent("<range>")}, r.file)
	if s.Tok != token.DEFINE {
		return noop
	}
	return func() {
		if had {
			r.locals[id.Name] = prev
			return
		}
		delete(r.locals, id.Name)
	}
}

// withProbe copies a probe map with one more binding, so an if's own init does
// not leak its probe into the statements that follow it.
func withProbe(probes map[string]ast.Expr, name string, path ast.Expr) map[string]ast.Expr {
	local := make(map[string]ast.Expr, len(probes)+1)
	for k, v := range probes {
		local[k] = v
	}
	local[name] = path
	return local
}

// switchSkips reads the CONDITIONS of a tag-less switch, which is an if-chain
// written with different punctuation:
//
//	switch {
//	case !cliPresent():
//	        t.Skip("cli/ is not here")
//	}
//
// The walker descended into a CaseClause's Body and never read its List, so the
// skip was seen and the guard was not, and the whole statement was not a site.
// Measured evading the gate on 2026-09-05 (B-188).
//
// A tagged switch is deliberately left alone: `switch x { case ok: }` compares
// values, so a bound predicate appearing there is not the predicate's own
// polarity and reading it would be a guess.
func (r *resolver) switchSkips(s *ast.SwitchStmt, probes map[string]ast.Expr) []siteWithPos {
	if s.Tag != nil || s.Body == nil {
		return nil
	}
	var (
		sites  []siteWithPos
		def    *ast.CaseClause
		conds  int
		sole   guardTarget
		soleM  bool
		soleOK bool
	)
	for _, cl := range s.Body.List {
		cc, ok := cl.(*ast.CaseClause)
		if !ok {
			continue
		}
		if len(cc.List) == 0 {
			def = cc
			continue
		}
		conds++
		var (
			tgt     guardTarget
			missing bool
			read    bool
		)
		for _, cond := range cc.List {
			if t, m, ok := r.pathCondition(cond, probes, 0); ok {
				tgt, missing, read = t, m, true
				break
			}
		}
		if !read {
			continue
		}
		// The clause runs when its condition holds, so a skip in it fires on a
		// MISSING path exactly when the condition is the missing-polarity one.
		if (missing || tgt.unreadable) && r.reaches(cc.Body) {
			sites = append(sites, r.newSite(tgt, stmtsSkipPos(cc.Body, cc.Pos())))
			continue
		}
		sole, soleM, soleOK = tgt, missing, true
	}
	// `switch { case cliPresent(): ... default: t.Skip() }` is the else arm of
	// one guard, read for the same reason skipArm reads both arms. Only with a
	// single condition: with two, which one the default answers is a guess.
	if soleOK && conds == 1 && def != nil && (!soleM || sole.unreadable) && r.reaches(def.Body) {
		sites = append(sites, r.newSite(sole, stmtsSkipPos(def.Body, def.Pos())))
	}
	return sites
}

// delegatedSkips reads a call to a same-package helper that carries a skip,
// with the caller's arguments bound into the helper's parameters:
//
//	func skipUnless(t *testing.T, ok bool, msg string) { if !ok { t.Skip(msg) } }
//	skipUnless(t, cliPresent(), "cli/ is not here")
//
// There is no `t.Skip` at the call site at all, so the site did not exist for
// this gate while the damage -- the whole test skips, everything under it dead
// -- was exactly the damage the gate is for. Measured evading it on 2026-09-05
// (B-188).
//
// The helper's body is walked, not guessed at: the polarity comes from the
// helper's own `if`, so `skipUnless` and `skipIf` are told apart by reading them
// rather than by their names.
func (r *resolver) delegatedSkips(call *ast.CallExpr, probes map[string]ast.Expr, seen map[string]bool, depth int) []siteWithPos {
	if depth >= maxDelegatedSkipDepth {
		return nil
	}
	h, name, ok := r.pkg.callee(call.Fun, r.file)
	if !ok || seen[name] {
		return nil
	}
	if !reachesSkip(h.decl.Body, r.pkg, h.file, map[string]bool{name: true}) {
		return nil
	}
	sub := &resolver{root: r.root, pkgDir: r.pkgDir, file: h.file, pkg: r.pkg}
	r.bindParams(sub, h.decl, call, depth)
	r.bindCondArgs(sub, h.decl, call, probes)
	next := make(map[string]bool, len(seen)+1)
	for k := range seen {
		next[k] = true
	}
	next[name] = true

	// A guard the HELPER writes for itself belongs to the helper, and a helper
	// declared in a _test.go file is already walked as a function in its own
	// right -- reporting it again at every call site turns one line of code into
	// one finding and one pin line per caller. Measured on 2026-09-05: a single
	// guarded helper with two callers produced three violations naming three
	// different functions for one guard. Only a guard the CALLER supplied is
	// reported here, plus everything inside a helper this scan never analyses on
	// its own (a non-test file in the same package), where the alternative is not
	// a duplicate but a silence.
	sites := guardedSkips(h.decl, sub, next, depth+1)
	if !strings.HasSuffix(h.file, "_test.go") {
		return sites
	}
	var fromCaller []siteWithPos
	for _, s := range sites {
		if s.fromCaller {
			fromCaller = append(fromCaller, s)
		}
	}
	return fromCaller
}

// bindCondArgs binds each parameter to the argument expression the CALLER wrote,
// carrying the caller's scope with it. A condition is only itself in the scope
// that spelled it: `cliPresent()`, `!cliPresent()` and a local `ok` are all the
// same guard handed across a call, and re-reading their names inside the
// helper's scope would answer about nothing.
func (r *resolver) bindCondArgs(sub *resolver, fd *ast.FuncDecl, call *ast.CallExpr, probes map[string]ast.Expr) {
	if fd.Type.Params == nil {
		return
	}
	i := 0
	for _, field := range fd.Type.Params.List {
		for _, pname := range field.Names {
			if i >= len(call.Args) {
				return
			}
			arg := call.Args[i]
			i++
			if pname.Name == "" || pname.Name == "_" {
				continue
			}
			if sub.condArgs == nil {
				sub.condArgs = map[string]boundCond{}
			}
			sub.condArgs[pname.Name] = boundCond{expr: arg, scope: r, probes: probes}
		}
	}
}

// reaches reports whether a list of statements reaches a skip call, directly or
// through a same-package helper.
func (r *resolver) reaches(stmts []ast.Stmt) bool {
	return reachesSkip(&ast.BlockStmt{List: stmts}, r.pkg, r.file, map[string]bool{})
}

// stmtsSkipPos is the position of the first skip call among the statements, or
// fallback when the skip is one call away and has no position here.
func stmtsSkipPos(stmts []ast.Stmt, fallback token.Pos) token.Pos {
	pos := fallback
	found := false
	ast.Inspect(&ast.BlockStmt{List: stmts}, func(n ast.Node) bool {
		if found {
			return false
		}
		if skipshape.IsSkipCall(n) {
			pos, found = n.Pos(), true
			return false
		}
		return true
	})
	return pos
}

// newSite resolves the guard's path expression into a classified site. The
// expression is resolved in the scope it was WRITTEN in, which for a predicate's
// probe is the predicate's own body and not the test's -- resolving `p` against
// the caller's `p` would answer about a different path with a straight face.
func (r *resolver) newSite(t guardTarget, pos token.Pos) siteWithPos {
	s := siteWithPos{pos: pos, fromCaller: t.fromCaller}
	scope := t.scope
	if scope == nil {
		scope = r
	}
	inFile := t.file
	if inFile == "" {
		inFile = scope.file
	}
	if s.Expr = t.desc; s.Expr == "" {
		s.Expr = scope.describe(t.expr)
	}
	if t.unreadable {
		s.Verdict = Unresolved
		return s
	}
	raw, kind := scope.resolve(t.expr, inFile, 0)
	switch kind {
	case kindExternal:
		s.Verdict = External
		return s
	case kindUnresolved:
		s.Verdict = Unresolved
		return s
	}
	// An absolute literal names a place on the machine. Joining it onto the
	// repository root produced "infra/tests/config/usr/bin/helmfile", a path that
	// cannot exist as spelled, and then judged the tree by it (B-183).
	var rel string
	absolute := false
	if anchored, ok := stripAnchor(raw); ok {
		rel = anchored
	} else if filepath.IsAbs(raw) || strings.HasPrefix(raw, "/") {
		absolute = true
		rel = raw
	} else {
		rel = filepath.Join(r.pkgDir, raw)
	}

	// WHERE IT LANDS, not how it is written. The first version of this decided
	// by string shape -- anything cleaning to a leading "../" was declared not
	// this repository's problem -- and a path that climbs OUT of the tree and
	// back IN under the repository's own directory name cleans to exactly that
	// while naming a file inside the tree. It escaped judgement entirely.
	//
	// Measured by an independent audit on 2026-09-05: the same absent file,
	// spelled "../../scripts/bootstrap-cluster.sh" and
	// "../../../../<repo-dir>/infra/scripts/bootstrap-cluster.sh", FAILED and
	// passed respectively. Fixing the fabricated path had traded it for a new
	// silence that anyone could reach on purpose.
	root, err := filepath.Abs(r.root)
	if err != nil {
		root = filepath.Clean(r.root)
	}
	full := filepath.Clean(rel)
	if !absolute {
		full = filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
	}
	inside, rerr := filepath.Rel(root, full)
	if rerr != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		// Genuinely elsewhere: a sibling checkout, a machine path. Reported as
		// it stands and not judged, because it is not a claim about this tree.
		s.Path = filepath.ToSlash(filepath.Clean(rel))
		s.Verdict = Outside
		return s
	}
	s.Path = filepath.ToSlash(inside)
	if _, err := os.Lstat(full); err == nil {
		s.Verdict = Present
	} else {
		s.Verdict = Absent
	}
	return s
}

// stripAnchor removes the repo-root marker, reporting whether it was there.
func stripAnchor(p string) (string, bool) {
	if p == rootAnchor {
		return ".", true
	}
	if rest, ok := strings.CutPrefix(p, rootAnchor+"/"); ok {
		return rest, true
	}
	return p, false
}

// probeBinding reports the error variable an fs probe binds and the path
// expression it was handed.
func probeBinding(a *ast.AssignStmt) (string, ast.Expr, bool) {
	if len(a.Rhs) != 1 || len(a.Lhs) == 0 {
		return "", nil, false
	}
	call, ok := a.Rhs[0].(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return "", nil, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", nil, false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", nil, false
	}
	fns, ok := fsProbes[pkg.Name]
	if !ok {
		return "", nil, false
	}
	argIdx, ok := fns[sel.Sel.Name]
	if !ok || argIdx >= len(call.Args) {
		return "", nil, false
	}
	// The error is the last result of every probe in the table.
	errIdent, ok := a.Lhs[len(a.Lhs)-1].(*ast.Ident)
	if !ok || errIdent.Name == "_" {
		return "", nil, false
	}
	return errIdent.Name, call.Args[argIdx], true
}

// guardTarget is the path a guard tests, together with the scope its expression
// must be read in. The scope is what makes a predicate safe to follow: the probe
// inside `cliSrcPresent()` reads that function's locals and its file's consts,
// not the test's.
type guardTarget struct {
	expr  ast.Expr
	scope *resolver
	file  string
	// desc overrides the rendered expression, so a predicate's site names the
	// predicate the reader will go looking for.
	desc string
	// unreadable marks a guard that reaches the filesystem through a predicate
	// this gate could not follow. It becomes an UNRESOLVED violation rather
	// than silence: a probe it cannot read is the one it must not pass.
	unreadable bool
	// fromCaller marks a guard the CALLER handed to a skip-carrying helper. It
	// decides where the finding belongs: a guard the caller supplied is the
	// caller's line to change, while a guard written inside the helper is the
	// helper's, and is reported there once instead of once per call site.
	fromCaller bool
}

// skipPos is the position of the first skip call in the block.
func skipPos(b *ast.BlockStmt) token.Pos {
	pos := b.Pos()
	ast.Inspect(b, func(n ast.Node) bool {
		if skipshape.IsSkipCall(n) {
			pos = n.Pos()
			return false
		}
		return true
	})
	return pos
}

// reachesSkip reports whether the block reaches a skip call -- itself, or
// through any same-package function or uniquely-named method it calls.
//
// TRANSITIVE for the same reason reachesFilesystemProbe is (B-186, B-188): the
// skip may have been moved one call away. `if !cliPresent() { bailOut(t) }`
// carries no `t.Skip` at the site, and reading the site alone made the whole
// guard vanish while the damage -- the test skips, everything below it is dead
// -- was unchanged. `seen` is keyed by function name and ends a cycle.
func reachesSkip(b *ast.BlockStmt, pkg *pkgScope, inFile string, seen map[string]bool) bool {
	if b == nil {
		return false
	}
	found := false
	ast.Inspect(b, func(n ast.Node) bool {
		if found {
			return false
		}
		if skipshape.IsSkipCall(n) {
			found = true
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		h, name, ok := pkg.callee(call.Fun, inFile)
		if !ok || seen[name] {
			return true
		}
		seen[name] = true
		if reachesSkip(h.decl.Body, pkg, h.file, seen) {
			found = true
			return false
		}
		return true
	})
	return found
}
