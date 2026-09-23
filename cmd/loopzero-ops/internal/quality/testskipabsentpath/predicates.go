// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testskipabsentpath

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

// here wraps an expression written in this resolver's own scope.
func (r *resolver) here(e ast.Expr) guardTarget {
	return guardTarget{expr: e, scope: r, file: r.file}
}

// skipArm reports the path an if-statement guards and WHICH ARM carries the
// skip that fires on that path being MISSING -- the only polarity a missing
// path can cause.
//
// Both arms are read (B-186). `if !present() { t.Skip() }` and
// `if present() { t.Fatal() } else { t.Skip() }` are the same guard written two
// ways, and reading only the then-arm made the second one silent while the doc
// declared the OPPOSITE polarity -- a skip that fires on PRESENCE -- as the
// deliberate non-site. That one still is: no missing path can cause it, in
// either arm.
func (r *resolver) skipArm(s *ast.IfStmt, probes map[string]ast.Expr) (guardTarget, *ast.BlockStmt, bool) {
	t, whenMissing, ok := r.pathCondition(s.Cond, probes, 0)
	if !ok {
		return guardTarget{}, nil, false
	}
	// An unreadable predicate has no polarity to trust, so it is reported
	// whichever way it is written. Withholding it under `if present()` would
	// hand back the evasion this floor exists to close.
	elseBlock, _ := s.Else.(*ast.BlockStmt)
	switch {
	case (whenMissing || t.unreadable) && r.armSkips(s.Body):
		return t, s.Body, true
	case (!whenMissing || t.unreadable) && r.armSkips(elseBlock):
		return t, elseBlock, true
	}
	return guardTarget{}, nil, false
}

// pathCondition reads a boolean condition and reports the path it tests plus
// whether it is true when that path is MISSING.
func (r *resolver) pathCondition(cond ast.Expr, probes map[string]ast.Expr, depth int) (guardTarget, bool, bool) {
	if depth > maxResolveDepth {
		return guardTarget{}, false, false
	}
	switch c := cond.(type) {
	case *ast.ParenExpr:
		return r.pathCondition(c.X, probes, depth+1)
	case *ast.Ident:
		// A parameter of a skip-carrying helper IS the caller's condition, read
		// back in the caller's scope (B-188). It is tried first because the
		// caller's spelling is the guard; the callee only decides its polarity.
		if bc, ok := r.condArgs[c.Name]; ok && bc.scope != nil {
			t, missing, read := bc.scope.pathCondition(bc.expr, bc.probes, depth+1)
			t.fromCaller = true
			return t, missing, read
		}
		// `ok, _ := cliSrcPresent()` then `if !ok { t.Skip() }`. The local IS
		// the predicate's answer, and this is the only spelling available to a
		// predicate returning (bool, error) -- Go has no other way to use one in
		// a condition (B-186).
		call, bound := r.boolPreds[c.Name]
		if !bound {
			return guardTarget{}, false, false
		}
		return r.guardCall(call.Fun, call, depth+1)
	case *ast.UnaryExpr:
		if c.Op != token.NOT {
			return guardTarget{}, false, false
		}
		t, missing, ok := r.pathCondition(c.X, probes, depth+1)
		return t, !missing, ok
	case *ast.BinaryExpr:
		switch c.Op {
		case token.EQL, token.NEQ:
			id, ok := c.X.(*ast.Ident)
			if !ok {
				return guardTarget{}, false, false
			}
			if nilIdent, ok := c.Y.(*ast.Ident); !ok || nilIdent.Name != "nil" {
				return guardTarget{}, false, false
			}
			p, ok := probes[id.Name]
			if !ok {
				return guardTarget{}, false, false
			}
			// `err != nil` is true when the probe failed; `err == nil` when it
			// found the path.
			return r.here(p), c.Op == token.NEQ, true
		case token.LAND, token.LOR:
			// `err == nil && info.IsDir()` is the shape in the tree: one side
			// carries the probe and the other narrows it. Two sides both
			// carrying a probe is ambiguous, and this gate does not guess.
			lt, lm, lok := r.pathCondition(c.X, probes, depth+1)
			rt, rm, rok := r.pathCondition(c.Y, probes, depth+1)
			switch {
			case lok && !rok:
				return lt, lm, true
			case rok && !lok:
				return rt, rm, true
			}
			return guardTarget{}, false, false
		}
		return guardTarget{}, false, false
	case *ast.CallExpr:
		// Parentheses are punctuation, not a reader. `(cliPresent)()` is stable
		// under gofmt and used to reach no reader at all (B-193).
		fn := unparen(c.Fun)
		if sel, ok := fn.(*ast.SelectorExpr); ok {
			if pkg, ok := sel.X.(*ast.Ident); ok && len(c.Args) > 0 &&
				((pkg.Name == "os" && sel.Sel.Name == "IsNotExist") ||
					(pkg.Name == "errors" && sel.Sel.Name == "Is")) {
				id, ok := c.Args[0].(*ast.Ident)
				if !ok {
					return guardTarget{}, false, false
				}
				p, ok := probes[id.Name]
				if !ok {
					return guardTarget{}, false, false
				}
				return r.here(p), true, true
			}
		}
		return r.guardCall(fn, c, depth)
	}
	return guardTarget{}, false, false
}

// unparen strips the parentheses a reader may write around a callee. gofmt
// keeps them, so `(cliPresent)()` is a stable spelling of a live guard; routing
// on the *ast.ParenExpr instead of what it wraps sent it to no reader at all
// and the gate said nothing (B-193).
func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// guardCall routes a call's CALLEE EXPRESSION to the reader that can answer it.
//
// It is one dispatcher rather than a switch inside pathCondition because the
// same routing has to happen twice: once for the callee as written, and again
// for the expression a NAME turns out to hold. `f := probes[k]; f()` is
// `probes[k]()` with one binding in between, and before B-193 the second
// spelling was UNRESOLVED while the first was silent -- one line of refactoring
// apart, and the line a reader writes first when factoring a guard out.
func (r *resolver) guardCall(fn ast.Expr, call *ast.CallExpr, depth int) (guardTarget, bool, bool) {
	if depth > maxResolveDepth {
		return guardTarget{}, false, false
	}
	fn = unparen(fn)
	switch f := fn.(type) {
	case *ast.SelectorExpr:
		return r.methodCondition(f, call, depth)
	case *ast.CallExpr:
		return r.returnedFuncCondition(f, call, depth)
	case *ast.Ident, *ast.IndexExpr, *ast.FuncLit:
		return r.predicateCondition(fn, call, depth)
	}
	return guardTarget{}, false, false
}

// predicateCondition reads a same-package predicate the guard calls -- the shape
// B-183 was opened for:
//
//	func cliSrcPresent() bool { info, err := os.Stat(cliSrcDir); return err == nil && info.IsDir() }
//	if !cliSrcPresent() { t.Skip("cli/ not present") }
//
// It returns the probed path and whether the predicate is TRUE when that path is
// missing. A predicate that reaches a probe but whose answer cannot be followed
// comes back marked unreadable, never as "not a site": that silence is exactly
// the hole a one-line refactor used to open.
// A name bound to a func VALUE rather than declared as a function is read the
// same way (B-189): `check := cliPresent` and `var alias = cliSrcPresent` are
// the same predicate reached through a binding, and looking only in pkg.funcs
// found neither.
// A NAME THAT HOLDS AN EXPRESSION is followed to the expression, and the
// expression is routed to the same readers the spelling without the binding
// goes to (B-193). That is the whole of root cause 1: the fallback that turns a
// silence into a verdict was reachable only through a package-level index, so
// `f := <anything red>; f()` was silent while `<anything red>()` was red.
func (r *resolver) predicateCondition(fn ast.Expr, call *ast.CallExpr, depth int) (guardTarget, bool, bool) {
	if h, key, ok := r.funcValue(fn, depth); ok {
		t, missing, read := r.predicateBody(key, h, call, depth)
		return r.attribute(t, fn), missing, read
	}
	if id, isID := fn.(*ast.Ident); isID {
		// A CROSS-FRAME binding is read in the caller's scope, and the call's
		// own arguments were written in this one. With no arguments there is
		// nothing to mis-resolve; with arguments, reading them over there would
		// answer about names this frame never bound -- a wrong path stated
		// confidently, which is worse than the UNRESOLVED the fallback gives.
		if b, scope, crossFrame, ok := r.heldExpr(id.Name); ok && (!crossFrame || len(call.Args) == 0) {
			// An expression that answers is the answer. One that does not falls
			// through to the name's own bar rather than returning a silence:
			// what the package binds under the NAME may still clear it, and
			// dropping that would have traded one silence for another.
			if t, missing, read := scope.guardCall(b.expr, call, depth+1); read {
				return r.attribute(t, fn), missing, read
			}
		}
	}
	return r.unreadableFuncValue(fn, depth)
}

// attribute marks a guard that came from the CALLER, so the finding lands on
// the line whose author has to change it rather than on the helper that
// received it -- the rule every delegated finding follows (B-188).
func (r *resolver) attribute(t guardTarget, fn ast.Expr) guardTarget {
	if !t.fromCaller && r.fromCallerScope(fn, 0) {
		t.fromCaller = true
	}
	return t
}

// fromCallerScope reports whether a name IS the caller's condition -- either the
// parameter the caller's value was bound to, or a local the helper bound that
// parameter to in turn.
//
// The second hop is not decoration. `func runWith(t, present func() bool) { p
// := present; if !p() {...} }` reads the caller's predicate correctly and then
// attributed the finding to the helper, where delegatedSkips drops every
// non-caller site to avoid reporting one guarded helper once per call -- so the
// whole guard came back as a PASS. Read, attributed to the wrong line, and
// therefore silent (B-193).
func (r *resolver) fromCallerScope(e ast.Expr, depth int) bool {
	if depth > maxResolveDepth || e == nil {
		return false
	}
	id, ok := unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	if _, ok := r.condArgs[id.Name]; ok {
		return true
	}
	if b, ok := r.locals[id.Name]; ok {
		return r.inFile(b.file).fromCallerScope(b.expr, depth+1)
	}
	return false
}

// heldExpr is the expression a NAME holds, together with the scope that wrote
// it: the condition a caller handed a helper, a local binding, or a
// package-level declaration. The third result says the scope belongs to another
// CALL FRAME, which is the caller's -- the one case where this resolver's own
// expressions and the returned scope do not share a vocabulary.
//
// A REBOUND package name is deliberately not followed. Its initializer stopped
// being the answer the moment something assigned over it, and reading it anyway
// is the confident verdict about a function that never ran that B-189 refused;
// unreadableFuncValue reports it instead.
func (r *resolver) heldExpr(name string) (boundExpr, *resolver, bool, bool) {
	if bc, ok := r.condArgs[name]; ok && bc.scope != nil {
		return boundExpr{expr: bc.expr, file: bc.scope.file}, bc.scope, true, true
	}
	if b, ok := r.locals[name]; ok {
		// A local an `=` reached from a branch or a closure is not followed, for
		// the reason a rebound package name is not: its last binding is where the
		// walk stopped, not where the guard read (B-193).
		if r.reboundLocals[name] {
			return boundExpr{}, nil, false, false
		}
		return b, r.inFile(b.file), false, true
	}
	if b, ok := r.pkg.consts[name]; ok && !r.pkg.rebound[name] {
		return b, r.inFile(b.file), false, true
	}
	return boundExpr{}, nil, false, false
}

// unreadableFuncValue is what a guard costs when it names a func value this gate
// cannot follow: an UNRESOLVED verdict, never a walk-past. The bar is the one
// every other unreadable guard here has to clear -- something the package binds
// to that name has to touch the filesystem -- so a table of cluster predicates
// stays none of this gate's business.
// THE REASON NAMES WHAT WAS MEASURED. It used to say "name rebound to another
// func value" for every identifier it refused, including one it refused for a
// different reason entirely -- a package name whose value this gate simply
// could not follow. A reason that asserts more than the check behind it sends
// the reader looking for an init() that is not there (B-193).
func (r *resolver) unreadableFuncValue(fn ast.Expr, depth int) (guardTarget, bool, bool) {
	switch x := unparen(fn).(type) {
	case *ast.Ident:
		// A LOCAL an `=` reached from a branch or a closure is answered first, and
		// answered about the values the frame actually wrote. It is checked before
		// the package indexes because a local SHADOWS them: asking pkg.rebound
		// about a name only some function reuses is asking about a different name.
		if r.reboundLocals[x.Name] {
			if !r.localReachesProbe(x.Name, depth) {
				return guardTarget{}, false, false
			}
			return guardTarget{desc: x.Name + "() (local assigned from a branch or a closure in this function; which value the guard reads is not decidable here)", unreadable: true}, false, true
		}
		reason := " (name rebound to another func value)"
		if !r.pkg.rebound[x.Name] {
			if _, declared := r.pkg.consts[x.Name]; !declared {
				return guardTarget{}, false, false
			}
			reason = " (name bound to a func value this gate could not follow)"
		}
		if !r.pkg.nameReachesProbe(x.Name) {
			return guardTarget{}, false, false
		}
		return guardTarget{desc: x.Name + "()" + reason, unreadable: true}, false, true
	case *ast.IndexExpr:
		// The container may be written INLINE rather than named:
		// `for _, f := range []func() bool{cliPresent}` binds f to an IndexExpr
		// whose X is the literal itself, and bindingOf only resolves names — so a
		// guard inside that loop was silent. Pre-existing, and the same family the
		// range spelling was taught for: found by testing the shape rather than
		// the spelling.
		var lit *ast.CompositeLit
		var inFile string
		if direct, isLit := unwrapCompositeLit(x.X); isLit {
			lit, inFile = direct, r.file
		} else {
			b, ok := r.bindingOf(x.X)
			if !ok {
				return guardTarget{}, false, false
			}
			var okLit bool
			lit, okLit = unwrapCompositeLit(b.expr)
			if !okLit {
				return guardTarget{}, false, false
			}
			inFile = b.file
		}
		if !r.inFile(inFile).containerReachesProbe(lit, depth) {
			return guardTarget{}, false, false
		}
		return guardTarget{desc: r.describe(x) + "() (entry of a table of predicates)", unreadable: true}, false, true
	}
	return guardTarget{}, false, false
}

// returnedFuncCondition reads `presentFn()()` -- a guard that calls the value
// another call returned.
//
// THE LIMIT IS DECLARED, not guessed at, because following a returned value
// means knowing where it came from. It is read only when the producing call
// resolves to a same-package body whose WHOLE body is one return, and that
// return's first result resolves to a func body by the same rules as any other
// func value. One unconditional return is knowing; a second return is a guess
// about which one ran, and this gate does not guess.
//
// Past that line the answer is UNRESOLVED and red, never silent -- and the bar
// for asking at all is the one the rest of the gate uses: the producer has to
// reach a filesystem probe. A factory that returns a cluster predicate stays
// none of this gate's business.
func (r *resolver) returnedFuncCondition(inner, outer *ast.CallExpr, depth int) (guardTarget, bool, bool) {
	producer, key, ok := r.funcValue(inner.Fun, depth+1)
	if !ok {
		return guardTarget{}, false, false
	}
	// THE BAR IS THE VALUES THE PRODUCER HANDS BACK, not the calls it makes.
	// `func presentFn() func() bool { return cliPresent }` calls nothing at all,
	// so asking its own body whether it reaches a probe answers about the wrong
	// function -- measured returning this exact spelling to silence while the
	// other five spellings were already red (2026-09-05).
	sub := &resolver{root: r.root, pkgDir: r.pkgDir, file: producer.file, pkg: r.pkg}
	body := producer.decl.Body
	reaches := reachesFilesystemProbe(body, r.pkg, map[string]bool{key: true})
	for _, ret := range returnedValues(body) {
		h, vkey, ok := sub.funcValue(ret, depth+1)
		if !ok || h.decl.Body == nil {
			continue
		}
		if reachesFilesystemProbe(h.decl.Body, r.pkg, map[string]bool{vkey: true}) {
			reaches = true
			break
		}
	}
	if !reaches {
		return guardTarget{}, false, false
	}

	// One unconditional return IS knowing where the value came from.
	if body != nil && len(body.List) == 1 {
		if ret, ok := body.List[0].(*ast.ReturnStmt); ok && len(ret.Results) > 0 {
			if h, vkey, ok := sub.funcValue(ret.Results[0], depth+1); ok {
				return r.predicateBody(key+"() -> "+vkey, h, outer, depth)
			}
		}
	}
	// A second return is a guess about which one ran, and so is a value
	// assembled at runtime or handed in from elsewhere. Said out loud, in red.
	return guardTarget{desc: key + "()() (value returned by a call)", unreadable: true}, false, true
}

// returnedValues is the FIRST result of every return statement in a body,
// without descending into function literals: a `return` inside a literal the
// body carries belongs to the literal, not to the function returning it.
func returnedValues(b *ast.BlockStmt) []ast.Expr {
	if b == nil {
		return nil
	}
	var out []ast.Expr
	var walk func(stmts []ast.Stmt)
	walk = func(stmts []ast.Stmt) {
		for _, st := range stmts {
			switch s := st.(type) {
			case *ast.ReturnStmt:
				if len(s.Results) > 0 {
					out = append(out, s.Results[0])
				}
			case *ast.BlockStmt:
				walk(s.List)
			case *ast.IfStmt:
				if s.Body != nil {
					walk(s.Body.List)
				}
				if s.Else != nil {
					walk([]ast.Stmt{s.Else})
				}
			case *ast.LabeledStmt:
				walk([]ast.Stmt{s.Stmt})
			case *ast.ForStmt:
				if s.Body != nil {
					walk(s.Body.List)
				}
			case *ast.RangeStmt:
				if s.Body != nil {
					walk(s.Body.List)
				}
			case *ast.SwitchStmt:
				if s.Body != nil {
					walk(s.Body.List)
				}
			case *ast.TypeSwitchStmt:
				if s.Body != nil {
					walk(s.Body.List)
				}
			case *ast.SelectStmt:
				if s.Body != nil {
					walk(s.Body.List)
				}
			case *ast.CaseClause:
				walk(s.Body)
			case *ast.CommClause:
				walk(s.Body)
			}
		}
	}
	walk(b.List)
	return out
}

// methodCondition reads a guard whose predicate is a METHOD -- `if !h.present()
// { t.Skip() }`. The receiver's type is unknown here (this package parses without
// type information), so the method is found by NAME and only when that name is
// unambiguous in the package and is not an import's function.
//
// An ambiguous name that touches the filesystem is UNRESOLVED, not a pass: which
// receiver's body runs is exactly the question this gate cannot answer, and a
// guess would be the silence it exists to remove. A path that lives in the
// receiver's own fields lands there too -- the resolver has no value to read it
// from, so it reports that rather than inventing one.
func (r *resolver) methodCondition(sel *ast.SelectorExpr, call *ast.CallExpr, depth int) (guardTarget, bool, bool) {
	if r.pkg.ambiguousMethod(sel, r.file) {
		return guardTarget{desc: "." + sel.Sel.Name + "()", unreadable: true}, false, true
	}
	// The SELECTOR is what names the method, not the call's own Fun: a local
	// that HOLDS `s.probe` is invoked as `p()`, and looking `p` up among this
	// package's functions would read a body the code never enters (B-193).
	if h, _, ok := r.pkg.callee(sel, r.file); ok {
		return r.predicateBody(sel.Sel.Name, h, call, depth)
	}
	// Not a method of this package. It may still be a FUNC VALUE held in a
	// field, and if the caller's own scope says which one, that is read (B-189).
	if h, key, ok := r.funcValue(sel, depth+1); ok {
		return r.predicateBody(key, h, call, depth)
	}
	// A func-typed field this gate cannot read the value of. WHICH value runs is
	// dataflow, which this gate does not have -- so it says so instead of walking
	// past. The bar is the same one every other unreadable guard has to clear:
	// something the package puts in that field touches the filesystem.
	if r.pkg.funcFieldReachesProbe(sel.Sel.Name) {
		return guardTarget{desc: "." + sel.Sel.Name + "() (func-valued field)", unreadable: true}, false, true
	}
	return guardTarget{}, false, false
}

// predicateBody reads one predicate declaration with the caller's arguments
// bound, and is shared by the function and method spellings of a guard.
func (r *resolver) predicateBody(name string, h helperDecl, call *ast.CallExpr, depth int) (guardTarget, bool, bool) {
	if h.decl.Body == nil || !returnsBoolFirst(h.decl) {
		return guardTarget{}, false, false
	}

	// The predicate's own scope, seeded with its parameters bound to whatever
	// the CALLER passed -- resolved here, in the caller's scope, then carried
	// across as a literal so the predicate's body cannot re-read the name.
	sub := &resolver{root: r.root, pkgDir: r.pkgDir, file: h.file, pkg: r.pkg}
	r.bindParams(sub, h.decl, call, depth)

	probes := map[string]ast.Expr{}
	var (
		early    *ast.IfStmt // if <cond> { return <bool literal> }
		earlyLit bool
		tail     ast.Expr // the last top-level return's expression
	)
	record := func(a *ast.AssignStmt) {
		sub.bindAssign(a)
		sub.bindBoolPred(a)
		if n, p, ok := probeBinding(a); ok {
			probes[n] = p
		}
	}
	for _, st := range h.decl.Body.List {
		switch s := st.(type) {
		case *ast.AssignStmt:
			record(s)
		case *ast.DeclStmt:
			sub.bindDecl(s)
		case *ast.IfStmt:
			if init, ok := s.Init.(*ast.AssignStmt); ok {
				record(init)
			}
			if lit, ok := singleBoolReturn(s.Body); ok && s.Else == nil {
				early, earlyLit = s, lit
			}
		case *ast.ReturnStmt:
			// The FIRST result, not the only one: a predicate returning
			// (bool, error) answers the same question with an error beside it,
			// and reading only single-result returns left that shape with no
			// tail to follow and therefore no site at all (B-186).
			if len(s.Results) > 0 {
				tail = s.Results[0]
			}
		}
	}
	if len(probes) == 0 && !reachesFilesystemProbe(h.decl.Body, r.pkg, map[string]bool{name: true}) {
		// Not a filesystem predicate at all, here or anywhere it delegates to.
		// `if !clusterReachable() { t.Skip() }` is honestly conditional on
		// something this gate has no opinion about.
		return guardTarget{}, false, false
	}

	unreadable := guardTarget{desc: name + "()", unreadable: true}
	describe := func(t guardTarget) guardTarget {
		// A target that already carries a description came from a predicate this
		// one delegates to; keeping the chain names every hop the reader has to
		// open, instead of rendering a parameter name in a scope that never
		// bound it.
		if t.desc != "" {
			// A chain that arrives back at this predicate is a cycle. Say that
			// once: printing the loop until the depth bound runs out buries the
			// one word the reader needs under two dozen repetitions of it.
			if strings.Contains(t.desc, name+"()") {
				t.desc = name + "() -> ... -> " + name + "() (cyclic)"
				return t
			}
			t.desc = name + "() -> " + t.desc
			return t
		}
		t.desc = name + "() -> " + sub.describe(t.expr)
		return t
	}

	// Shape 1: the answer IS the condition -- `return err == nil`.
	if tail != nil {
		if lit, isLit := boolLit(tail); !isLit {
			t, missing, ok := sub.pathCondition(tail, probes, depth+1)
			if !ok {
				return unreadable, false, true
			}
			return describe(t), missing, true
		} else if early != nil && lit != earlyLit {
			// Shape 2: `if <cond> { return X }` then `return !X`. The predicate
			// is X exactly when the condition holds.
			t, missing, ok := sub.pathCondition(early.Cond, probes, depth+1)
			if !ok {
				return unreadable, false, true
			}
			if !earlyLit {
				missing = !missing
			}
			return describe(t), missing, true
		}
	}
	return unreadable, false, true
}

// bindParams resolves each argument in the CALLER's scope and binds it, as a
// literal, to the parameter name inside the predicate's scope. An argument that
// does not resolve is left unbound: the predicate may not need it, and a guess
// would be worse than an unresolved verdict.
func (r *resolver) bindParams(sub *resolver, fd *ast.FuncDecl, call *ast.CallExpr, depth int) {
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
			if v, kind := r.resolve(arg, r.file, depth+1); kind == kindResolved {
				sub.bind(pname.Name, &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(v)}, r.file)
			}
		}
	}
}

// returnsBoolFirst reports whether fd's FIRST result is a bool.
//
// It used to demand exactly one result, which rejected `(bool, error)` before
// the filesystem-probe check ever ran -- so the UNRESOLVED floor never got a
// chance and the shape evaded in silence (B-186). A predicate answering
// "is it there?" with an error beside the answer is the same predicate.
func returnsBoolFirst(fd *ast.FuncDecl) bool {
	if fd.Type.Results == nil || len(fd.Type.Results.List) == 0 {
		return false
	}
	f := fd.Type.Results.List[0]
	if len(f.Names) > 1 {
		return false
	}
	id, ok := f.Type.(*ast.Ident)
	return ok && id.Name == "bool"
}

// boolLit reports the value of a `true`/`false` literal.
func boolLit(e ast.Expr) (bool, bool) {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false, false
	}
	switch id.Name {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// singleBoolReturn reports the literal of a block whose only statement returns
// a bool literal FIRST. `return false, err` is that same early answer with an
// error beside it (B-186).
func singleBoolReturn(b *ast.BlockStmt) (bool, bool) {
	if b == nil || len(b.List) != 1 {
		return false, false
	}
	ret, ok := b.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) == 0 {
		return false, false
	}
	return boolLit(ret.Results[0])
}

// reachesFilesystemProbe reports whether the block calls one of the probes in
// fsProbes -- itself, or through any same-package function it calls. It is the
// trigger for the unreadable-predicate floor, kept narrow on purpose: a
// predicate that never touches the filesystem, at any depth, is none of this
// gate's business.
//
// TRANSITIVE is the whole of B-186. This used to read the block ALONE, so
// `present() { return dirExists(p) }` reached no probe of its own and the
// caller bailed before ever reading the predicate's answer: not UNRESOLVED,
// silent. An independent audit walked the class back in with exactly that
// two-function refactor on 2026-09-05 and the gate printed PASS.
//
// `seen` is keyed by function name and must arrive holding the name of the
// function b belongs to. It makes the walk exact rather than truncated: every
// reachable body is read once, and a cycle ends the walk having read them all,
// so a `false` here means "no probe anywhere reachable" and never "I stopped
// early". The bound that CAN truncate is on following the answer, not on
// finding the probe, and truncation there is UNRESOLVED, never silence.
func reachesFilesystemProbe(b *ast.BlockStmt, pkg *pkgScope, seen map[string]bool) bool {
	if b == nil {
		return false
	}
	found := false
	ast.Inspect(b, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			pkgIdent, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			if fns, ok := fsProbes[pkgIdent.Name]; ok {
				if _, ok := fns[sel.Sel.Name]; ok {
					found = true
					return false
				}
			}
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || seen[id.Name] {
			return true
		}
		// Package scope only: a func VALUE bound at package level resolves here
		// too, so a predicate that delegates through `var alias = cliSrcPresent`
		// reaches the probe the alias reaches (B-189). A local binding needs the
		// caller's scope, which a reachability walk does not carry.
		h, key, ok := (&resolver{pkg: pkg}).funcValue(id, 0)
		if !ok || h.decl.Body == nil || seen[key] {
			return true
		}
		seen[id.Name] = true
		seen[key] = true
		if reachesFilesystemProbe(h.decl.Body, pkg, seen) {
			found = true
			return false
		}
		return true
	})
	return found
}

// armSkips reports whether an if-arm reaches a skip call -- written there, or
// one same-package call away. `if !cliPresent() { bailOut(t) }` is the same
// guard as the one that spells the skip inline (B-188).
func (r *resolver) armSkips(b *ast.BlockStmt) bool {
	if b == nil {
		return false
	}
	return reachesSkip(b, r.pkg, r.file, map[string]bool{})
}
