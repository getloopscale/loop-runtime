// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package testskipabsentpath

import (
	"go/ast"
	"go/token"
)

// indexFuncFields records this file's func-typed struct fields and every func
// value the file assigns to a field, by field NAME.
//
// By name, for the same reason methods are indexed by name: without type
// information there is no way to say which struct a `s.probe` belongs to. The
// name is enough for the only two questions asked of this index -- "does this
// package declare a func-typed field spelled this way?" and "does anything it
// is ever given touch the filesystem?" -- and neither answer is a verdict on
// its own.
func indexFuncFields(pkg *pkgScope, pf parsedFile) {
	ast.Inspect(pf.ast, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.StructType:
			if x.Fields == nil {
				return true
			}
			for _, f := range x.Fields.List {
				if !pkg.isFuncType(f.Type) {
					continue
				}
				for _, name := range f.Names {
					pkg.funcFields[name.Name] = true
				}
			}
		case *ast.CompositeLit:
			for _, elt := range x.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.Ident); ok {
					pkg.boundFuncs[key.Name] = append(pkg.boundFuncs[key.Name],
						boundExpr{expr: kv.Value, file: pf.rel})
				}
			}
		case *ast.AssignStmt:
			// ASSIGN, not DEFINE: `f = other` gives an existing name a second
			// value, which is what makes the name's declaration stop being the
			// answer. `f := other` introduces a new one.
			//
			// This records the VALUE only, under the name as spelled. Whether
			// the name is the package's or a local shadowing it is decided by
			// indexRebindings, because only that answer gets printed.
			if x.Tok != token.ASSIGN || len(x.Lhs) != len(x.Rhs) {
				return true
			}
			for i, lhs := range x.Lhs {
				switch target := lhs.(type) {
				case *ast.SelectorExpr:
					pkg.boundFuncs[target.Sel.Name] = append(pkg.boundFuncs[target.Sel.Name],
						boundExpr{expr: x.Rhs[i], file: pf.rel})
				case *ast.Ident:
					pkg.boundFuncs[target.Name] = append(pkg.boundFuncs[target.Name],
						boundExpr{expr: x.Rhs[i], file: pf.rel})
				}
			}
		}
		return true
	})
}

// indexFuncTypes records the package's NAMED func types, so a struct field
// declared through one is recognised as func-typed.
//
// `type predicate func() bool` then `probe predicate` is the same field as
// `probe func() bool`, and matching only the literal *ast.FuncType left it out
// of funcFields entirely -- which meant the bar an unreadable field has to
// clear ("something the package puts in that field touches the filesystem")
// could be MET and the guard still walked past in silence. A property the
// documentation claimed and the code did not have (B-193).
func indexFuncTypes(pkg *pkgScope, pf parsedFile) {
	for _, decl := range pf.ast.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if _, ok := ts.Type.(*ast.FuncType); ok {
				pkg.funcTypes[ts.Name.Name] = true
			}
		}
	}
}

// isFuncType reports whether a field's declared type is a func type this
// package can name: the literal `func(...)` or one of its own named func types.
func (p *pkgScope) isFuncType(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.FuncType:
		return true
	case *ast.Ident:
		return p.funcTypes[t.Name]
	}
	return false
}

// indexRebindings records the PACKAGE-LEVEL names this file gives a second
// value to after their declaration -- and nothing else.
//
// WHY IT NEEDS SCOPES AT ALL. `pkg.rebound` is the one index here that turns
// into a printed sentence: a name in it is refused by funcValue and reported
// "name rebound to another func value". It used to be set by any `=` to an
// identifier anywhere in the package, locals included, so
//
//	var present = cliSrcPresent          // never reassigned
//	func unrelated() { present := ...; present = other }   // a LOCAL
//
// downgraded the alias to UNRESOLVED and printed a confident claim about a
// rebinding that did not happen (B-193). A false reason is worse than a missing
// one: the reader goes looking for an init() that is not there.
//
// WHAT IT TRACKS. Go's own block scopes, in declaration order: a name declared
// by `:=`, by a `var`/`const` inside a body, or as a receiver, parameter,
// result, range variable or type-switch binding is LOCAL from that point to the
// end of the block that declared it. An `=` to such a name rebinds the local
// and says nothing about the package-level name it shadows. An `=` to any other
// identifier is a package-level rebinding, which is exactly what this index is
// for -- an `init()` swapping an alias is the shape B-189 measured.
//
// It walks function literals with the enclosing scopes still visible, because a
// closure assigning to a captured local is rebinding that local, not the
// package's name.
func indexRebindings(pkg *pkgScope, pf parsedFile) {
	for _, decl := range pf.ast.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Body == nil {
				continue
			}
			w := &rebindWalker{pkg: pkg}
			w.push()
			w.declare(d.Recv)
			w.declare(d.Type.Params)
			w.declare(d.Type.Results)
			w.stmts(d.Body.List)
			w.pop()
		case *ast.GenDecl:
			// A package-level `var x = func() { ... }` carries statements too.
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				w := &rebindWalker{pkg: pkg}
				w.push()
				for _, v := range vs.Values {
					w.expr(v)
				}
				w.pop()
			}
		}
	}
}

// rebindWalker carries a stack of block scopes over one function body.
type rebindWalker struct {
	pkg    *pkgScope
	scopes []map[string]bool
}

func (w *rebindWalker) push() { w.scopes = append(w.scopes, map[string]bool{}) }
func (w *rebindWalker) pop()  { w.scopes = w.scopes[:len(w.scopes)-1] }

// local reports whether the name is declared in any scope currently open.
func (w *rebindWalker) local(name string) bool {
	for _, s := range w.scopes {
		if s[name] {
			return true
		}
	}
	return false
}

// bind declares a name in the innermost open scope.
func (w *rebindWalker) bind(name string) {
	if name == "" || name == "_" || len(w.scopes) == 0 {
		return
	}
	w.scopes[len(w.scopes)-1][name] = true
}

// declare binds every name in a parameter, result or receiver list.
func (w *rebindWalker) declare(fl *ast.FieldList) {
	if fl == nil {
		return
	}
	for _, f := range fl.List {
		for _, n := range f.Names {
			w.bind(n.Name)
		}
	}
}

// bindLHS declares the identifiers a `:=` introduces.
func (w *rebindWalker) bindLHS(lhs []ast.Expr) {
	for _, e := range lhs {
		if id, ok := e.(*ast.Ident); ok {
			w.bind(id.Name)
		}
	}
}

// expr walks an expression for the function literals inside it, which carry
// statements of their own. It stops at each literal rather than descending: the
// literal's body is walked by w.stmts, in a scope of its own.
func (w *rebindWalker) expr(e ast.Expr) {
	if e == nil {
		return
	}
	ast.Inspect(e, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if !ok || lit.Body == nil {
			return true
		}
		w.push()
		w.declare(lit.Type.Params)
		w.declare(lit.Type.Results)
		w.stmts(lit.Body.List)
		w.pop()
		return false
	})
}

func (w *rebindWalker) exprs(es []ast.Expr) {
	for _, e := range es {
		w.expr(e)
	}
}

// block walks a statement list in a scope of its own.
func (w *rebindWalker) block(b *ast.BlockStmt) {
	if b == nil {
		return
	}
	w.push()
	w.stmts(b.List)
	w.pop()
}

// stmt walks one statement in the CURRENT scope, which is what makes
// declaration order matter: a name is local only from its declaration onward.
func (w *rebindWalker) stmts(stmts []ast.Stmt) {
	for _, st := range stmts {
		w.stmt(st)
	}
}

func (w *rebindWalker) stmt(st ast.Stmt) {
	switch s := st.(type) {
	case nil:
		return
	case *ast.AssignStmt:
		w.exprs(s.Rhs)
		if s.Tok == token.DEFINE {
			w.bindLHS(s.Lhs)
			return
		}
		w.exprs(s.Lhs)
		if s.Tok != token.ASSIGN || len(s.Lhs) != len(s.Rhs) {
			return
		}
		for _, lhs := range s.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Name == "_" || w.local(id.Name) {
				continue
			}
			w.pkg.rebound[id.Name] = true
		}
	case *ast.DeclStmt:
		gd, ok := s.Decl.(*ast.GenDecl)
		if !ok {
			return
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			w.exprs(vs.Values)
			for _, n := range vs.Names {
				w.bind(n.Name)
			}
		}
	case *ast.BlockStmt:
		w.block(s)
	case *ast.LabeledStmt:
		w.stmt(s.Stmt)
	case *ast.ExprStmt:
		w.expr(s.X)
	case *ast.SendStmt:
		w.expr(s.Chan)
		w.expr(s.Value)
	case *ast.IncDecStmt:
		w.expr(s.X)
	case *ast.GoStmt:
		w.expr(s.Call)
	case *ast.DeferStmt:
		w.expr(s.Call)
	case *ast.ReturnStmt:
		w.exprs(s.Results)
	case *ast.IfStmt:
		w.push()
		w.stmt(s.Init)
		w.expr(s.Cond)
		w.block(s.Body)
		w.stmt(s.Else)
		w.pop()
	case *ast.ForStmt:
		w.push()
		w.stmt(s.Init)
		w.expr(s.Cond)
		w.stmt(s.Post)
		w.block(s.Body)
		w.pop()
	case *ast.RangeStmt:
		w.push()
		w.expr(s.X)
		if s.Tok == token.DEFINE {
			w.bindLHS(nonNilExprs(s.Key, s.Value))
		} else {
			// `for k = range xs` gives an EXISTING name a second value, and if
			// that name is the package's, it is a rebinding like any other.
			for _, e := range nonNilExprs(s.Key, s.Value) {
				if id, ok := e.(*ast.Ident); ok && id.Name != "_" && !w.local(id.Name) {
					w.pkg.rebound[id.Name] = true
				}
			}
		}
		w.block(s.Body)
		w.pop()
	case *ast.SwitchStmt:
		w.push()
		w.stmt(s.Init)
		w.expr(s.Tag)
		w.block(s.Body)
		w.pop()
	case *ast.TypeSwitchStmt:
		w.push()
		w.stmt(s.Init)
		w.stmt(s.Assign)
		w.block(s.Body)
		w.pop()
	case *ast.SelectStmt:
		w.block(s.Body)
	case *ast.CaseClause:
		w.push()
		w.exprs(s.List)
		w.stmts(s.Body)
		w.pop()
	case *ast.CommClause:
		w.push()
		w.stmt(s.Comm)
		w.stmts(s.Body)
		w.pop()
	}
}

// nonNilExprs drops the absent halves of a range clause.
func nonNilExprs(es ...ast.Expr) []ast.Expr {
	var out []ast.Expr
	for _, e := range es {
		if e != nil {
			out = append(out, e)
		}
	}
	return out
}

type helperDecl struct {
	decl *ast.FuncDecl
	file string // repo-relative file the helper is declared in
}

// boundExpr is an expression plus the file it was written in, which is what
// the runtime.Caller idiom resolves to.
type boundExpr struct {
	expr ast.Expr
	file string
}

// pkgScope is the binding scope shared by every file of one package directory.
type pkgScope struct {
	// helpers are the zero-argument funcs a PATH expression may call.
	helpers map[string]helperDecl
	// funcs is every non-method func in the package, by name. A guard's
	// predicate is looked up here rather than in helpers because a presence
	// predicate may take the path as an argument.
	funcs map[string]helperDecl
	// methods is every METHOD in the package, keyed by method name, carrying
	// every declaration that name has. A guard written `if !h.present() {...}`
	// names a function this gate can read; what it cannot read without type
	// information is WHICH receiver's, so the index keeps them all and callee()
	// refuses the ambiguous ones rather than picking one (B-188).
	methods map[string][]helperDecl
	// imports is the set of import names visible in each file, so a call
	// spelled `pkg.Present()` is never mistaken for a method of this package
	// that happens to share the name. Without it the resolver would read a body
	// the code does not call, which is a guess wearing a verdict's clothes.
	imports map[string]map[string]bool
	consts  map[string]boundExpr
	// funcTypes is the set of NAMED func types this package declares --
	// `type predicate func() bool`. It exists so funcFields can recognise a
	// field declared through the name as the func-typed field it is; matching
	// only a literal *ast.FuncType left `probe predicate` unindexed, and an
	// unindexed field is a silence rather than the UNRESOLVED the declared bar
	// asks for (B-193).
	funcTypes map[string]bool
	// funcFields is the set of STRUCT FIELD names this package declares with a
	// func type. boundFuncs is every value the package is seen assigning to a
	// NAME, by any spelling -- a composite-literal key, an `x.f = v`, a plain
	// `f = v`. rebound is the package-level names that get a second value after
	// their declaration.
	//
	// None of the four ever produces a verdict on its own. They answer one
	// question -- "does anything this package puts under that name touch the
	// filesystem?" -- which is the bar an UNREADABLE guard has to clear before
	// it is reported UNRESOLVED instead of walked past (B-189). Keying by name
	// alone can pool a field and a var that share one; that costs an UNRESOLVED
	// the reader can pin, which is the direction this gate errs on purpose.
	//
	// rebound is the exception that had to be made exact, because it is the one
	// that PRINTS A CLAIM: "name rebound to another func value". It used to be
	// set by any `=` to an identifier anywhere in the package, locals included,
	// so an alias nobody ever reassigned was downgraded to UNRESOLVED under a
	// sentence asserting a rebinding that did not happen (B-193). It is filled
	// by indexRebindings, which carries Go's own block scopes.
	funcFields map[string]bool
	boundFuncs map[string][]boundExpr
	rebound    map[string]bool
}

// funcValueCandidates is every expression this package is seen binding to the
// name: its package-level initializer, plus every later assignment.
func (p *pkgScope) funcValueCandidates(name string) []boundExpr {
	var out []boundExpr
	if b, ok := p.consts[name]; ok {
		out = append(out, b)
	}
	return append(out, p.boundFuncs[name]...)
}

// nameReachesProbe reports whether any value this package binds to the name
// resolves to a body that touches the filesystem.
func (p *pkgScope) nameReachesProbe(name string) bool {
	for _, v := range p.funcValueCandidates(name) {
		r := &resolver{file: v.file, pkg: p}
		h, key, ok := r.funcValue(v.expr, 0)
		if !ok || h.decl.Body == nil {
			continue
		}
		if reachesFilesystemProbe(h.decl.Body, p, map[string]bool{key: true}) {
			return true
		}
	}
	return false
}

// callee resolves a call to the same-package body it enters, if this gate can
// name that body without type information. It returns the declaration and a key
// that is unique per body, for the visited sets that end a cycle.
//
// A plain `f()` is looked up among the package's functions. A `x.m()` is looked
// up among its METHODS, and only when the name is unambiguous: `sel.X` may be an
// imported package (rejected via the file's import set) and two types may
// declare the same method (rejected as ambiguous). Refusing is not silence here
// -- pathCondition turns the ambiguous case into an UNRESOLVED verdict when the
// bodies touch the filesystem at all.
//
// It reads DECLARATIONS only. A name bound to a func value is resolved by
// resolver.funcValue instead, which needs the caller's locals and so cannot live
// on the package scope (B-189). The two are kept apart on purpose: this one
// answers where a SKIP is carried, where following a value is still a declared
// limit, and that one answers what a GUARD calls.
func (p *pkgScope) callee(callee ast.Expr, inFile string) (helperDecl, string, bool) {
	switch fn := unparen(callee).(type) {
	case *ast.Ident:
		h, ok := p.funcs[fn.Name]
		if !ok || h.decl.Body == nil {
			return helperDecl{}, "", false
		}
		return h, fn.Name, true
	case *ast.SelectorExpr:
		if !p.methodIsOurs(fn, inFile) {
			return helperDecl{}, "", false
		}
		ms := p.methods[fn.Sel.Name]
		if len(ms) != 1 || ms[0].decl.Body == nil {
			return helperDecl{}, "", false
		}
		return ms[0], "." + fn.Sel.Name, true
	}
	return helperDecl{}, "", false
}

// funcFieldReachesProbe reports whether this package declares a func-typed field
// with this name AND ever puts a filesystem-touching func value in it. It is the
// bar that keeps an unreadable field from becoming an opinion about conditions
// this gate has none about: `if !c.ready() { t.Skip() }` over a field nothing
// fills with a probe stays none of its business, exactly as
// `if !dialCluster() { t.Skip() }` does.
func (p *pkgScope) funcFieldReachesProbe(name string) bool {
	return p.funcFields[name] && p.nameReachesProbe(name)
}

// methodIsOurs reports whether `x.m` can name a method of this package: the
// method name must be declared here, and the receiver expression must not be an
// import name visible in the file that wrote it. An unknown file (no import set)
// is treated as unknown, not as ours.
func (p *pkgScope) methodIsOurs(sel *ast.SelectorExpr, inFile string) bool {
	if len(p.methods[sel.Sel.Name]) == 0 {
		return false
	}
	imports, known := p.imports[inFile]
	if !known {
		return false
	}
	if id, ok := sel.X.(*ast.Ident); ok && imports[id.Name] {
		return false
	}
	return true
}

// ambiguousMethod reports whether `x.m` names methods of this package that this
// gate cannot tell apart, and whether any of them touches the filesystem. A
// guard on one of those is UNRESOLVED, never a pass.
func (p *pkgScope) ambiguousMethod(sel *ast.SelectorExpr, inFile string) bool {
	if !p.methodIsOurs(sel, inFile) || len(p.methods[sel.Sel.Name]) < 2 {
		return false
	}
	for _, m := range p.methods[sel.Sel.Name] {
		if reachesFilesystemProbe(m.decl.Body, p, map[string]bool{}) {
			return true
		}
	}
	return false
}
