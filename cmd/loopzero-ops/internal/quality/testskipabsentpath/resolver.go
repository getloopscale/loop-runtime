// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testskipabsentpath

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
)

type resolveKind int

const (
	kindUnresolved resolveKind = iota
	kindResolved
	kindExternal
)

// funcValue resolves an expression that NAMES A FUNCTION VALUE to the body a
// call on it enters. It is the whole of B-189.
//
// `pkgScope.callee` answers the same question for a call spelled as a
// declaration -- `f()` against pkg.funcs, `x.m()` against pkg.methods -- and a
// name bound to a func VALUE is in neither index, so it was not found, not read
// and not reported. Six spellings walked past the gate in silence on 2026-09-05,
// each one a one-line refactor from the live `if !cliSrcPresent() { t.Skip() }`:
//
//	var alias = cliSrcPresent            // package alias
//	var f = func() bool { ...os.Stat...} // package var holding a literal
//	check := cliPresent                  // local
//	p := h.Present                       // method value
//	s.probe                              // struct field holding a func
//
// Resolution order is Go's own shadowing order -- locals, then package-level
// vars and consts, then declarations -- so a local that shadows a package func
// is read as the local, not as the name it hides.
//
// The returned key names the body for the reader and for the visited sets that
// end a cycle. Nothing here guesses: an expression this cannot follow comes back
// false, and the caller decides whether that costs a verdict or a silence.
func (r *resolver) funcValue(e ast.Expr, depth int) (helperDecl, string, bool) {
	if depth > maxResolveDepth || e == nil {
		return helperDecl{}, "", false
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		return r.funcValue(x.X, depth+1)
	case *ast.Ident:
		// A PARAMETER of a skip-carrying helper is the CALLER's func value, read
		// back in the caller's scope. `run(t, cliPresent)` over
		// `func run(t *testing.T, present func() bool) { if !present() {...} }`
		// puts the guard at the call and nowhere else, and reading `present` in
		// the helper's own scope answers about nothing.
		if bc, ok := r.condArgs[x.Name]; ok && bc.scope != nil {
			return bc.scope.funcValue(bc.expr, depth+1)
		}
		if b, ok := r.locals[x.Name]; ok {
			// A LOCAL an `=` reached from a branch or a closure answers with none
			// of its values. Which assignment the guard read is dataflow -- a
			// branch that may not run, a closure that runs afterwards -- and this
			// gate has none. Refused here so the caller reports UNRESOLVED,
			// exactly as a rebound package-level name is two branches down. A
			// name every statement before the guard certainly assigned is NOT in
			// this set, and still resolves in order (B-193).
			if r.reboundLocals[x.Name] {
				return helperDecl{}, "", false
			}
			if h, key, ok := r.inFile(b.file).funcValue(b.expr, depth+1); ok {
				return h, key, true
			}
			return helperDecl{}, "", false
		}
		if b, ok := r.pkg.consts[x.Name]; ok {
			// A package-level name that is ASSIGNED again after its declaration
			// no longer answers with its initializer: `var present = alwaysTrue`
			// swapped in an init() to `cliSrcPresent` runs the second one, and
			// reading the first is how the swap stayed silent. Refused here so
			// the caller reports UNRESOLVED.
			if r.pkg.rebound[x.Name] {
				return helperDecl{}, "", false
			}
			if h, key, ok := r.inFile(b.file).funcValue(b.expr, depth+1); ok {
				return h, key, true
			}
			return helperDecl{}, "", false
		}
		h, ok := r.pkg.funcs[x.Name]
		if !ok || h.decl.Body == nil {
			return helperDecl{}, "", false
		}
		return h, x.Name, true
	case *ast.IndexExpr:
		// `probes["cli"]()` -- a table of predicates. Read only when the
		// container is a composite literal this scope binds AND the key is a
		// literal that names one of its entries. Anything else is a guess about
		// which entry ran, and the caller turns it into UNRESOLVED.
		b, ok := r.bindingOf(x.X)
		if !ok {
			return helperDecl{}, "", false
		}
		lit, ok := unwrapCompositeLit(b.expr)
		if !ok {
			return helperDecl{}, "", false
		}
		want, ok := x.Index.(*ast.BasicLit)
		if !ok {
			return helperDecl{}, "", false
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			got, ok := kv.Key.(*ast.BasicLit)
			if !ok || got.Kind != want.Kind || got.Value != want.Value {
				continue
			}
			return r.inFile(b.file).funcValue(kv.Value, depth+1)
		}
		return helperDecl{}, "", false
	case *ast.FuncLit:
		if x.Body == nil {
			return helperDecl{}, "", false
		}
		// A literal has no name of its own. It is wrapped in a synthetic
		// declaration so every reader downstream -- returnsBoolFirst,
		// predicateBody, reachesFilesystemProbe -- reads it exactly as it reads
		// a declared predicate, with no second code path to keep in step.
		return helperDecl{
			decl: &ast.FuncDecl{Name: ast.NewIdent("func literal"), Type: x.Type, Body: x.Body},
			file: r.file,
		}, "func literal", true
	case *ast.SelectorExpr:
		// A METHOD VALUE -- `p := h.Present` -- is the method, found by the same
		// name rule a called method is found by.
		if r.pkg.methodIsOurs(x, r.file) && !r.pkg.ambiguousMethod(x, r.file) {
			ms := r.pkg.methods[x.Sel.Name]
			if len(ms) == 1 && ms[0].decl.Body != nil {
				return ms[0], "." + x.Sel.Name, true
			}
			return helperDecl{}, "", false
		}
		// A FIELD holding a func, read from the composite literal the caller's
		// own scope bound to the receiver. This is the only field read that is
		// not a guess: `s := holder{probe: cliPresent}` says which func value
		// `s.probe` holds, right there, in the scope that asks. A receiver from
		// anywhere else is dataflow, and the caller turns that into UNRESOLVED
		// rather than reading some other assignment to the same field name.
		recv, ok := x.X.(*ast.Ident)
		if !ok {
			return helperDecl{}, "", false
		}
		b, ok := r.locals[recv.Name]
		if !ok {
			return helperDecl{}, "", false
		}
		lit, ok := unwrapCompositeLit(b.expr)
		if !ok {
			return helperDecl{}, "", false
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != x.Sel.Name {
				continue
			}
			return r.inFile(b.file).funcValue(kv.Value, depth+1)
		}
		return helperDecl{}, "", false
	}
	return helperDecl{}, "", false
}

// inFile is this resolver seen from another file of the same package. A binding
// carries the file it was written in, and a func literal's declaring file is
// what a finding has to name.
func (r *resolver) inFile(file string) *resolver {
	if file == "" || file == r.file {
		return r
	}
	return &resolver{root: r.root, pkgDir: r.pkgDir, file: file, pkg: r.pkg,
		locals: r.locals, boolPreds: r.boolPreds, condArgs: r.condArgs,
		localBinds: r.localBinds, reboundLocals: r.reboundLocals}
}

// unwrapCompositeLit reads `holder{...}` and `&holder{...}` alike.
func unwrapCompositeLit(e ast.Expr) (*ast.CompositeLit, bool) {
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = u.X
	}
	lit, ok := e.(*ast.CompositeLit)
	return lit, ok
}

// containerReachesProbe reports whether any element of a composite literal of
// predicates resolves to a body that touches the filesystem. It is the bar for
// `probes[k]()` whose key this gate cannot read.
func (r *resolver) containerReachesProbe(lit *ast.CompositeLit, depth int) bool {
	for _, elt := range lit.Elts {
		v := elt
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			v = kv.Value
		}
		h, key, ok := r.funcValue(v, depth+1)
		if !ok || h.decl.Body == nil {
			continue
		}
		if reachesFilesystemProbe(h.decl.Body, r.pkg, map[string]bool{key: true}) {
			return true
		}
	}
	return false
}

// bindingOf is the expression a name is bound to, local first, then package.
func (r *resolver) bindingOf(e ast.Expr) (boundExpr, bool) {
	id, ok := e.(*ast.Ident)
	if !ok {
		return boundExpr{}, false
	}
	if b, ok := r.locals[id.Name]; ok {
		return b, true
	}
	b, ok := r.pkg.consts[id.Name]
	return b, ok
}

// resolver turns a path expression into a string, using only bindings it can
// see. Anything it cannot follow comes back unresolved -- never as a guess.
type resolver struct {
	root   string
	pkgDir string
	file   string
	pkg    *pkgScope
	locals map[string]boundExpr
	// boolPreds maps a local to the same-package predicate call that produced
	// it, so `ok, _ := cliSrcPresent()` followed by `if !ok { t.Skip() }` reads
	// as the guard it is. It is a separate map from locals because the value is
	// an ANSWER, not a path, and resolving it as a path would be a category
	// error dressed as a verdict.
	boolPreds map[string]*ast.CallExpr
	// condArgs maps a parameter of a skip-carrying helper to the condition the
	// CALLER handed it, together with the caller's scope. It is how
	// `skipUnless(t, cliPresent(), "...")` is read as the guard it is (B-188).
	condArgs map[string]boundCond
	// localBinds is every expression this frame is seen binding to a local name,
	// in the order the walk met them. locals holds only the LAST; localBinds is
	// what the probe BAR is asked over, so a name is judged on the values that
	// were actually written to it rather than on the one the walk passed last.
	//
	// reboundLocals is the set of names an `=` reached from somewhere this frame
	// cannot promise ran -- an if arm, a loop body, a deferred closure. For those
	// the gate says UNRESOLVED instead of picking one, which is noteAssign's
	// whole subject.
	//
	// condDepth is how deep the walk currently is inside such a place. It is on
	// the resolver rather than threaded through the walker because bindAssign is
	// reached from six statement cases and a seventh would have been the one that
	// forgot.
	localBinds    map[string][]boundExpr
	reboundLocals map[string]bool
	condDepth     int
}

// boundCond is a boolean expression carried across a call boundary: the
// expression as the CALLER wrote it, plus the scope and probes that give its
// names meaning. Re-reading it in the callee's scope would answer about
// different bindings with the same confidence.
type boundCond struct {
	expr   ast.Expr
	scope  *resolver
	probes map[string]ast.Expr
}

func (r *resolver) bind(name string, e ast.Expr, file string) {
	if name == "" || name == "_" {
		return
	}
	if r.locals == nil {
		r.locals = map[string]boundExpr{}
	}
	b := boundExpr{expr: e, file: file}
	if r.localBinds == nil {
		r.localBinds = map[string][]boundExpr{}
	}
	r.localBinds[name] = append(r.localBinds[name], b)
	r.locals[name] = b
}

// noteAssign decides whether an `=` leaves this frame still able to say what a
// name holds when the guard reads it.
//
// STATEMENT ORDER ANSWERS ONLY WHERE EVERY STATEMENT RUNS. `f := alwaysTrue; f
// = cliPresent; if !f() {...}` is decidable and this gate reads it: the second
// assignment is a plain statement of the function body, so it has certainly run
// by the time the guard does, and the value the guard calls is the one it gets.
// A test pins exactly that, and it refused an earlier, coarser version of this
// rule that made every twice-bound name UNRESOLVED -- rightly, because that
// answer was available (B-193).
//
// WHAT IS NOT DECIDABLE is an `=` the walk met somewhere that may not have run
// before the guard: inside an if arm, a loop or switch body, or inside a
// function literal that `defer`, `t.Cleanup` or any other call runs later or
// never. Measured 2026-09-06, all four were SILENT and one was worse --
// `f := cliPresent; if cond { f = otherPresent }; if !f() {...}` reported ABSENT
// naming otherPresent's path, because the walk is textual and the last `=` it
// passed won. That is a confident verdict about the wrong file, which this gate
// refuses everywhere else it can name the refusal; naming it here is the whole
// of this round.
//
// An unconditional `=` CLEARS the doubt rather than accumulating it: whatever a
// branch did earlier, this one certainly overwrote it.
func (r *resolver) noteAssign(a *ast.AssignStmt) {
	if a.Tok != token.ASSIGN {
		// `:=` declares. It is the name's first value in its own scope, not a
		// second value for one that already had it.
		return
	}
	for _, lhs := range a.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok || id.Name == "_" {
			continue
		}
		if r.condDepth == 0 {
			delete(r.reboundLocals, id.Name)
			continue
		}
		if _, had := r.locals[id.Name]; !had {
			continue
		}
		if r.reboundLocals == nil {
			r.reboundLocals = map[string]bool{}
		}
		r.reboundLocals[id.Name] = true
	}
}

// underCondition walks a statement list whose execution this frame cannot
// promise -- an if arm, a loop or switch body, a func literal someone else
// decides when to run. An `=` met in there is a value the name MAY hold, and
// noteAssign records it as the doubt it is.
func (r *resolver) underCondition(walk func()) {
	r.condDepth++
	walk()
	r.condDepth--
}

// localCandidates is every expression this frame put under a local name. It is
// the bar for a name whose value the frame cannot pin down: the same bar every
// unreadable guard here clears, asked over the values that were actually
// written rather than over the one the walk happened to pass last.
func (r *resolver) localCandidates(name string) []boundExpr {
	return r.localBinds[name]
}

// localReachesProbe reports whether any value this frame bound to the name
// resolves to a body that touches the filesystem. It is nameReachesProbe's
// question asked of a LOCAL, and it is what keeps a rebound `dialFn` out of
// this gate's business while a rebound `f := cliPresent` comes back UNRESOLVED.
func (r *resolver) localReachesProbe(name string, depth int) bool {
	for _, v := range r.localCandidates(name) {
		h, key, ok := r.inFile(v.file).funcValue(v.expr, depth+1)
		if !ok || h.decl.Body == nil {
			continue
		}
		if reachesFilesystemProbe(h.decl.Body, r.pkg, map[string]bool{key: true}) {
			return true
		}
	}
	return false
}

// bindAssign records `p := "lit"`, `root, err := repoRoot()` and the
// runtime.Caller idiom.
func (r *resolver) bindAssign(a *ast.AssignStmt) {
	r.noteAssign(a)
	if len(a.Rhs) == 1 {
		if call, ok := a.Rhs[0].(*ast.CallExpr); ok {
			if isRuntimeCaller(call) {
				// _, thisFile, _, ok := runtime.Caller(0): result 1 is the file
				// the call is WRITTEN in, anchored at the repository root.
				if len(a.Lhs) >= 2 {
					if id, ok := a.Lhs[1].(*ast.Ident); ok {
						lit := &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(rootAnchor + "/" + r.file)}
						r.bind(id.Name, lit, r.file)
					}
				}
				return
			}
			if len(a.Lhs) > 1 {
				// `root, err := helper()` -- bind the FIRST result.
				if id, ok := a.Lhs[0].(*ast.Ident); ok {
					r.bind(id.Name, call, r.file)
				}
				return
			}
		}
	}
	if len(a.Lhs) != len(a.Rhs) {
		return
	}
	for i, lhs := range a.Lhs {
		if id, ok := lhs.(*ast.Ident); ok {
			r.bind(id.Name, a.Rhs[i], r.file)
		}
	}
}

// bindBoolPred records `ok, _ := cliSrcPresent()` -- a local holding the answer
// of a same-package predicate whose FIRST result is a bool. Only the first
// result is bound: it is the one the guard reads.
func (r *resolver) bindBoolPred(a *ast.AssignStmt) {
	if len(a.Rhs) != 1 || len(a.Lhs) == 0 {
		return
	}
	call, ok := a.Rhs[0].(*ast.CallExpr)
	if !ok {
		return
	}
	// Any spelling that names a body: a declared predicate, or a name bound to a
	// func VALUE (B-189). `check := cliSrcPresent` then `ok, _ := check()` is the
	// same answer reached through one more binding.
	h, _, ok := r.funcValue(call.Fun, 0)
	if !ok || h.decl.Body == nil || !returnsBoolFirst(h.decl) {
		return
	}
	name, ok := a.Lhs[0].(*ast.Ident)
	if !ok || name.Name == "_" {
		return
	}
	if r.boolPreds == nil {
		r.boolPreds = map[string]*ast.CallExpr{}
	}
	r.boolPreds[name.Name] = call
}

// bindDecl records `const p = "lit"` / `var p = "lit"` inside a function.
func (r *resolver) bindDecl(d *ast.DeclStmt) {
	gd, ok := d.Decl.(*ast.GenDecl)
	if !ok {
		return
	}
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok || len(vs.Names) != len(vs.Values) {
			continue
		}
		for i, name := range vs.Names {
			r.bind(name.Name, vs.Values[i], r.file)
		}
	}
}

// resolve turns e into a path. `inFile` is the file e was written in, which
// decides what runtime.Caller means for it.
func (r *resolver) resolve(e ast.Expr, inFile string, depth int) (string, resolveKind) {
	if depth > maxResolveDepth {
		return "", kindUnresolved
	}
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", kindUnresolved
		}
		s, err := strconv.Unquote(x.Value)
		if err != nil {
			return "", kindUnresolved
		}
		return s, kindResolved
	case *ast.Ident:
		if b, ok := r.locals[x.Name]; ok {
			// A PATH local carries the same doubt a func-value local does, and
			// it was measured carrying it: `p := alphaDir; if cond { p = betaDir
			// }; os.Stat(p)` reported ABSENT naming betaDir, and the `defer`
			// spelling of the same thing reported betaDir for an assignment that
			// had provably NOT run yet. The first is a guess, the second is
			// simply wrong, and both wore the confident ABSENT verdict this gate
			// reserves for a path it read. UNRESOLVED is the honest answer and it
			// is still red (B-193).
			if r.reboundLocals[x.Name] {
				return "", kindUnresolved
			}
			return r.resolve(b.expr, b.file, depth+1)
		}
		if b, ok := r.pkg.consts[x.Name]; ok {
			return r.resolve(b.expr, b.file, depth+1)
		}
		return "", kindUnresolved
	case *ast.ParenExpr:
		return r.resolve(x.X, inFile, depth+1)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", kindUnresolved
		}
		l, lk := r.resolve(x.X, inFile, depth+1)
		rr, rk := r.resolve(x.Y, inFile, depth+1)
		if lk == kindExternal || rk == kindExternal {
			return "", kindExternal
		}
		if lk != kindResolved || rk != kindResolved {
			return "", kindUnresolved
		}
		return l + rr, kindResolved
	case *ast.CallExpr:
		return r.resolveCall(x, inFile, depth)
	}
	return "", kindUnresolved
}

// resolveCall follows filepath.Join / filepath.Dir and same-package zero-arg
// helpers whose first return is itself resolvable -- which is how `repoRoot()`
// resolves without this gate hard-coding its name. os.Getenv marks the whole
// expression EXTERNAL: a path rooted in the environment is not a claim about
// this repository.
func (r *resolver) resolveCall(call *ast.CallExpr, inFile string, depth int) (string, resolveKind) {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return "", kindUnresolved
		}
		if pkg.Name == "os" && (sel.Sel.Name == "Getenv" || sel.Sel.Name == "TempDir" || sel.Sel.Name == "UserHomeDir") {
			return "", kindExternal
		}
		if pkg.Name != "filepath" && pkg.Name != "path" {
			return "", kindUnresolved
		}
		switch sel.Sel.Name {
		case "Join":
			parts := make([]string, 0, len(call.Args))
			for _, a := range call.Args {
				p, k := r.resolve(a, inFile, depth+1)
				if k == kindExternal {
					return "", kindExternal
				}
				if k != kindResolved {
					return "", kindUnresolved
				}
				parts = append(parts, p)
			}
			return filepath.ToSlash(filepath.Join(parts...)), kindResolved
		case "Dir":
			if len(call.Args) != 1 {
				return "", kindUnresolved
			}
			p, k := r.resolve(call.Args[0], inFile, depth+1)
			if k != kindResolved {
				return "", k
			}
			return filepath.ToSlash(filepath.Dir(p)), kindResolved
		case "Clean":
			if len(call.Args) != 1 {
				return "", kindUnresolved
			}
			p, k := r.resolve(call.Args[0], inFile, depth+1)
			if k != kindResolved {
				return "", k
			}
			return filepath.ToSlash(filepath.Clean(p)), kindResolved
		}
		return "", kindUnresolved
	}

	id, ok := call.Fun.(*ast.Ident)
	if !ok || len(call.Args) != 0 {
		return "", kindUnresolved
	}
	h, ok := r.pkg.helpers[id.Name]
	if !ok || h.decl.Body == nil {
		return "", kindUnresolved
	}
	// The helper gets its own binding scope, seeded from its own body and
	// anchored to the file it is declared in.
	sub := &resolver{root: r.root, pkgDir: r.pkgDir, file: h.file, pkg: r.pkg}
	var ret ast.Expr
	var walk func(stmts []ast.Stmt)
	walk = func(stmts []ast.Stmt) {
		for _, st := range stmts {
			switch s := st.(type) {
			case *ast.AssignStmt:
				sub.bindAssign(s)
			case *ast.DeclStmt:
				sub.bindDecl(s)
			case *ast.IfStmt:
				if s.Body != nil {
					walk(s.Body.List)
				}
			case *ast.ReturnStmt:
				// The LAST return with a non-empty, non-error first result is
				// the helper's answer; an early `return "", err` guard is not.
				if len(s.Results) > 0 {
					if lit, ok := s.Results[0].(*ast.BasicLit); ok && lit.Kind == token.STRING && lit.Value == `""` {
						continue
					}
					ret = s.Results[0]
				}
			}
		}
	}
	walk(h.decl.Body.List)
	if ret == nil {
		return "", kindUnresolved
	}
	return sub.resolve(ret, h.file, depth+1)
}

func isRuntimeCaller(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "runtime" && sel.Sel.Name == "Caller"
}

// describe renders the guard's expression for a human. A bare identifier says
// almost nothing on its own, so when one is bound in scope the binding is shown
// with it -- the reader needs to see what could not be resolved, not its name.
func (r *resolver) describe(e ast.Expr) string {
	id, ok := e.(*ast.Ident)
	if !ok {
		return exprString(e)
	}
	// A name this frame bound from a branch or a closure has no single value to
	// show. `locals` holds the last one the walk passed, and printing it as
	// `p (= betaDir)` beside "could not resolve that expression" names one
	// candidate with the confidence of a reading -- the same overreach the
	// rebound REASON was corrected for. Say how many there were instead.
	if r.reboundLocals[id.Name] {
		return id.Name + " (assigned from a branch or a closure in this function; which value the guard reads is not decidable here)"
	}
	b, ok := r.locals[id.Name]
	if !ok {
		if b, ok = r.pkg.consts[id.Name]; !ok {
			return id.Name
		}
	}
	inner := exprString(b.expr)
	if inner == id.Name || inner == "<expr>" {
		return id.Name
	}
	return id.Name + " (= " + inner + ")"
}

// exprString renders an expression for a human-readable message.
func exprString(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.BasicLit:
		return x.Value
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprString(x.X) + "." + x.Sel.Name
	case *ast.CallExpr:
		args := make([]string, 0, len(x.Args))
		for _, a := range x.Args {
			args = append(args, exprString(a))
		}
		return exprString(x.Fun) + "(" + strings.Join(args, ", ") + ")"
	case *ast.BinaryExpr:
		return exprString(x.X) + " " + x.Op.String() + " " + exprString(x.Y)
	case *ast.ParenExpr:
		return "(" + exprString(x.X) + ")"
	case *ast.IndexExpr:
		return exprString(x.X) + "[" + exprString(x.Index) + "]"
	}
	return "<expr>"
}
