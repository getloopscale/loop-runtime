// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package testskipabsentpath implements the `test-skip-absent-path` gate.
//
// PROBLEM (B-180, generalising B-169). A test guards itself with a filesystem
// check and skips when the path is missing:
//
//	if _, err := os.Stat("../../../cli/src/index.ts"); os.IsNotExist(err) {
//	        t.Skipf("cli/ moved to sibling loopzero-cli/ repo per M5 decomposition")
//	}
//	// ... assertions that never run ...
//
// That is conditional in FORM and unconditional in FACT: `cli/` is not in this
// tree and the sibling is not cloned, so the skip branch is the only branch
// ever taken. The package still reports `ok`, the skip line is invisible
// without `-v`, and everything below the guard is unreachable code that reads
// like a live assertion -- one of the four skips B-180 removed carried an
// unguarded os.ReadFile and a t.Fatalf below it.
//
// WHAT THIS GATE ANSWERS. For every path-guarded skip under infra/tests/,
// exactly one of four things, named in the output:
//
//  1. CONDITIONAL -- the guarded path exists in this tree. The guard is
//     genuinely conditional, the test runs here, nothing to declare.
//  2. ABSENT and UNDECLARED -- the skip is unconditional in fact. FAIL. The
//     fix is to remove the test naming the guard that covers the contract
//     instead (B-169), or to declare it in the pin with a written reason.
//  3. TRACKED -- an absence declared in the pin. Printed on EVERY run, pass or
//     fail, so a tracked absence is never quieter than a fresh one.
//  4. EXTERNAL -- the guard's path is rooted outside this repository (a
//     $HOME/... backup directory, say). The gate states that it does not and
//     cannot assert about it, rather than reporting a clean verdict over it.
//
// A pin line whose path came BACK also fails: the guard is conditional again
// and the declaration has become stale prose.
//
// WHY IT CANNOT BECOME WHAT IT GUARDS. The failure mode of a scanner is
// finding nothing and calling that clean. Four floors close that:
//
//   - the scan directory must exist -- a moved infra/tests/ is an error, not a
//     pass;
//   - the scan must find at least one *_test.go file;
//   - the scan must find at least one skip call in the whole tree. If the AST
//     walk stops matching the shape of a skip, that is a broken detector, not
//     a clean repository;
//   - EVERY pin line must be re-found by this run's scan. The pin holds live
//     anchors: when the detector silently stops detecting, every anchor goes
//     unmatched and the gate goes red naming them. A declaration nobody
//     re-finds is the detector's own obituary.
//
// None of the four is ever reported as a pass, which is the property that
// protects. They do not share an exit code, and the difference is deliberate:
// the first three are SETUP BROKEN (exit 5) because the gate could not run; the
// pin re-find is a FAIL (exit 1) because the gate ran and found a declaration
// that protects nothing. An earlier draft of this comment called all four a
// BROKEN SETUP, which sent a reader auditing the floors by exit code looking for
// a fifth 5 that is not there.
//
// SCOPE, stated so it is not mistaken for more.
//
//   - It detects a skip whose GUARD is a filesystem existence probe. It does
//     NOT detect a bare unconditional `t.Skip(...)` with no guard: that shape
//     is at least honest about being unconditional, its damage (dead
//     assertions below it) is a different defect, and classifying WHY a bare
//     skip exists means reading its prose.
//
//   - A guard on a path rooted in an environment variable is reported EXTERNAL
//     and not judged. This is a declared limit, not an oversight: such a path
//     is not a claim about this repository, and a gate that guessed about
//     $HOME would fail on one machine and pass on the next.
//
//   - THE BIGGEST LIMIT, and the one that bounds what this gate may be said to
//     prove: the guard must be the filesystem probe ITSELF. A probe moved one
//     level into a boolean helper --
//
//     func cliSrcPresent() bool { _, err := os.Stat(cliSrcDir); return err == nil }
//     if !cliSrcPresent() { t.Skip("cli/ not present") }
//
//     -- is not recognised, because the guard expression is a call and not a
//     probe. Path helpers ARE resolved (repoRoot() and friends, below); boolean
//     presence helpers are not. This is not hypothetical: it is the shape at
//     api/internal/contract/cli_consumer_test.go, where two permanent cli/ skips
//     sit unflagged, and a one-line refactor of any test in scope reopens the
//     class here. So what this gate stops is the INLINE SPELLING of the class,
//     not the class. Demonstrated by an independent audit on 2026-09-05 by
//     injecting exactly that shape into infra/tests/config/; the gate passed it.
//     Closing it is B-183.
//
//   - An ABSOLUTE literal path is joined onto the repo root and reported under a
//     path that cannot exist: os.Stat("/usr/bin/helmfile") is named as
//     "infra/tests/config/usr/bin/helmfile ... ABSENT from this tree". EXTERNAL
//     exists for out-of-repo roots but recognises only variable-based ones. No
//     instance is live under infra/tests/ today; scanning cmd/ produces 11
//     violations dominated by this class. Also B-183.
package testskipabsentpath

import (
	"bufio"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// DefaultScanDir is the tree this gate is responsible for, repo-relative.
const DefaultScanDir = "infra/tests"

// DefaultPinFile is the declaration of tracked path-guarded skips. It sits in
// infra/quality/ beside the other pinned contracts this repo keeps for its own
// test governance (test-pyramid-baseline.json, skip-link-baseline.txt).
const DefaultPinFile = "infra/quality/test-skip-absent-path.txt"

// maxResolveDepth bounds the expression resolver against a cyclic binding.
const maxResolveDepth = 24

// rootAnchor marks a resolved path as already rooted at the repository root,
// as opposed to relative to the test's package directory. A path built from
// runtime.Caller carries it; a bare "../../foo" literal does not, and the two
// must not be measured from the same place. It is deliberately not path-shaped
// so filepath.Join and filepath.Clean cannot eat it.
const rootAnchor = "__LOOPZERO_REPO_ROOT__"

// Config drives the gate.
type Config struct {
	// RepoRoot is this repository's root. Defaults to ".".
	RepoRoot string
	// ScanDir overrides DefaultScanDir (repo-relative).
	ScanDir string
	// PinFile overrides DefaultPinFile (repo-relative).
	PinFile string
}

// Verdict is what the gate concluded about one guard's path.
type Verdict int

const (
	// Unresolved: the gate could not turn the guard's expression into a path.
	Unresolved Verdict = iota
	// External: the path is rooted outside this repository.
	External
	// Present: the path exists in this tree, so the guard is conditional.
	Present
	// Absent: the path is missing, so the skip always fires.
	Absent
)

// Site is one detected path-guarded skip.
type Site struct {
	// File is the test file, repo-relative with forward slashes.
	File string
	// Func is the enclosing function declaration's name. Together with File
	// and Path it is the pin key -- stable under edits that move lines.
	Func string
	// Line is where the skip call sits, for the human reading the output.
	Line int
	// Expr is the guard's path expression as written, for messages.
	Expr string
	// Path is the resolved repo-relative path. Empty unless Verdict is
	// Present or Absent.
	Path string
	// Verdict is the gate's conclusion about Path.
	Verdict Verdict
}

// Key is the pin key for this site.
func (s Site) Key() string { return s.File + "::" + s.Func + "::" + s.Path }

// Pin is one declared, tracked path-guarded skip.
type Pin struct {
	File   string
	Func   string
	Path   string
	Reason string
	Line   int
}

// Key is the site key this pin claims to cover.
func (p Pin) Key() string { return p.File + "::" + p.Func + "::" + p.Path }

// Outcome is the verdict.
type Outcome struct {
	Pass bool

	// ScanRoot is the directory that was walked, as resolved.
	ScanRoot string
	// FilesScanned and SkipCalls are the floors' measurements, reported so a
	// reader can see the gate had a subject.
	FilesScanned int
	SkipCalls    int

	// Sites is every path-guarded skip found, sorted by file then line.
	Sites []Site
	// Conditional, Tracked and Externals are the non-failing classifications,
	// each printed on every run.
	Conditional []string
	Tracked     []string
	Externals   []string
	// Violations is what makes the gate fail.
	Violations []string
}

// The anti-vacuity floors. Each is a BROKEN SETUP, not a clean tree.
var (
	errNoTestFiles = errors.New("found no *_test.go file -- a scan over an empty population reports PASS while asserting nothing")
	errNoSkipCalls = errors.New("found no skip call at all -- either every test in the tree lost its skip, or this gate's AST detector no longer matches the shape of one, and a silent zero here turns the whole gate into a no-op")
)

// fsProbes are the calls whose error result reports whether a path exists,
// mapped to the index of their path argument. Open/OpenFile are here because a
// test that opens-and-skips guards on existence exactly as one that
// stats-and-skips does.
var fsProbes = map[string]map[string]int{
	"os": {
		"Stat":     0,
		"Lstat":    0,
		"Open":     0,
		"OpenFile": 0,
		"ReadFile": 0,
		"ReadDir":  0,
	},
	"ioutil": {
		"ReadFile": 0,
		"ReadDir":  0,
	},
}

// Check runs the gate. It returns an error only for a broken SETUP (a missing
// scan tree, an unreadable or malformed pin, a detector that found nothing);
// a violated contract comes back as Outcome.Violations with Pass false.
func Check(cfg Config) (*Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	scanDir := cfg.ScanDir
	if scanDir == "" {
		scanDir = DefaultScanDir
	}
	pinFile := cfg.PinFile
	if pinFile == "" {
		pinFile = DefaultPinFile
	}

	scanRoot := filepath.Join(root, scanDir)
	info, err := os.Stat(scanRoot)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("the scan tree %s is not a directory (%v) -- this gate has no subject, "+
			"which is a broken setup and not a clean repository", scanRoot, err)
	}

	pins, err := parsePins(filepath.Join(root, pinFile))
	if err != nil {
		return nil, err
	}

	out := &Outcome{ScanRoot: scanRoot}
	if err := scan(out, root, scanRoot); err != nil {
		return nil, err
	}
	if out.FilesScanned == 0 {
		return nil, fmt.Errorf("%s: %w", scanRoot, errNoTestFiles)
	}
	if out.SkipCalls == 0 {
		return nil, fmt.Errorf("%s: %w", scanRoot, errNoSkipCalls)
	}

	classify(out, pins, pinFile)
	out.Pass = len(out.Violations) == 0
	return out, nil
}

// classify joins the scan against the pin and writes the verdict.
func classify(out *Outcome, pins []Pin, pinFile string) {
	byKey := make(map[string]Pin, len(pins))
	for _, p := range pins {
		byKey[p.Key()] = p
	}
	matched := make(map[string]bool, len(pins))

	for _, s := range out.Sites {
		switch s.Verdict {
		case Unresolved:
			out.Violations = append(out.Violations, fmt.Sprintf(
				"%s:%d: %s skips on a filesystem check of `%s`, and this gate could not resolve that "+
					"expression to a path -- so it cannot say whether the skip is conditional or permanent, "+
					"and it will not report clean over what it did not read. Make the path a literal (or a "+
					"literal joined under a helper in the same package)",
				s.File, s.Line, s.Func, s.Expr))
		case External:
			out.Externals = append(out.Externals, fmt.Sprintf(
				"%s:%d %s -> `%s` (rooted outside this repository; NOT judged)",
				s.File, s.Line, s.Func, s.Expr))
		case Present:
			if pin, pinned := byKey[s.Key()]; pinned {
				matched[s.Key()] = true
				out.Violations = append(out.Violations, fmt.Sprintf(
					"%s:%d declares %s::%s on %s, but that path EXISTS in this tree -- the guard is "+
						"conditional again and the declaration has become stale prose. Remove the line and "+
						"let the test assert",
					pinFile, pin.Line, s.File, s.Func, s.Path))
				continue
			}
			out.Conditional = append(out.Conditional,
				fmt.Sprintf("%s::%s -> %s", s.File, s.Func, s.Path))
		case Absent:
			pin, pinned := byKey[s.Key()]
			if !pinned {
				out.Violations = append(out.Violations, fmt.Sprintf(
					"%s:%d: %s skips when %s is missing, and that path is ABSENT from this tree -- the skip "+
						"is conditional in form and unconditional in fact, so every assertion below it is "+
						"dead. Remove the test naming the guard that covers the contract instead (B-169), or "+
						"declare it in %s as a TAB-separated line:\n      %s\t%s\t%s\t<why, naming what covers this instead>",
					s.File, s.Line, s.Func, s.Path, pinFile, s.File, s.Func, s.Path))
				continue
			}
			matched[s.Key()] = true
			out.Tracked = append(out.Tracked,
				fmt.Sprintf("%s::%s -> %s (%s)", s.File, s.Func, s.Path, pin.Reason))
		}
	}

	// Every pin must be re-found. This is what stops the gate from dying in
	// silence: if the detector stops detecting, the anchors go unmatched.
	for _, p := range pins {
		if matched[p.Key()] {
			continue
		}
		out.Violations = append(out.Violations, fmt.Sprintf(
			"%s:%d declares a path-guarded skip in %s::%s on %s, and this run's scan did NOT find it. "+
				"Either the skip was removed (delete this line) or this gate stopped recognising it "+
				"(fix the detector) -- a declaration nobody re-finds protects nothing",
			pinFile, p.Line, p.File, p.Func, p.Path))
	}
}

// ---------------------------------------------------------------------------
// scanning
// ---------------------------------------------------------------------------

// scan walks the tree one PACKAGE DIRECTORY at a time. Per-directory rather
// than per-file because a test's path helper often lives in a non-test file of
// the same package (chartrender's repoRoot does), and a per-file resolver would
// report those guards unresolvable for a reason that is about this gate rather
// than about the tree.
func scan(out *Outcome, root, scanRoot string) error {
	var dirs []string
	err := filepath.WalkDir(scanRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		switch d.Name() {
		case ".git", "node_modules", "vendor", "testdata", ".cache":
			return fs.SkipDir
		}
		dirs = append(dirs, path)
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		if err := scanDir(out, root, dir); err != nil {
			return err
		}
	}
	sort.Slice(out.Sites, func(i, j int) bool {
		if out.Sites[i].File != out.Sites[j].File {
			return out.Sites[i].File < out.Sites[j].File
		}
		return out.Sites[i].Line < out.Sites[j].Line
	})
	return nil
}

// parsedFile carries a file's AST plus the repo-relative path it was read
// from, which the runtime.Caller idiom resolves to.
type parsedFile struct {
	rel  string
	fset *token.FileSet
	ast  *ast.File
}

func scanDir(out *Outcome, root, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var files []parsedFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		rel, rerr := filepath.Rel(root, full)
		if rerr != nil {
			rel = full
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, full, nil, parser.SkipObjectResolution)
		if perr != nil {
			// An unparseable file is a fact about the tree this gate must not
			// swallow: it is precisely the file whose guards would go unread.
			return fmt.Errorf("parsing %s: %w", filepath.ToSlash(rel), perr)
		}
		files = append(files, parsedFile{rel: filepath.ToSlash(rel), fset: fset, ast: f})
	}
	if len(files) == 0 {
		return nil
	}

	// Package-wide binding scope: zero-argument helpers and file-level
	// const/var literals, from every file in the directory.
	pkg := &pkgScope{helpers: map[string]helperDecl{}, consts: map[string]boundExpr{}}
	for _, pf := range files {
		for _, decl := range pf.ast.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok {
				if fd.Recv == nil && fd.Type.Params.NumFields() == 0 {
					pkg.helpers[fd.Name.Name] = helperDecl{decl: fd, file: pf.rel}
				}
				continue
			}
			gd, ok := decl.(*ast.GenDecl)
			if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != len(vs.Values) {
					continue
				}
				for i, name := range vs.Names {
					pkg.consts[name.Name] = boundExpr{expr: vs.Values[i], file: pf.rel}
				}
			}
		}
	}

	pkgDir := filepath.ToSlash(mustRel(root, dir))
	for _, pf := range files {
		if !strings.HasSuffix(pf.rel, "_test.go") {
			continue
		}
		out.FilesScanned++
		analyseFile(out, root, pkgDir, pkg, pf)
	}
	return nil
}

func mustRel(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return dir
	}
	return rel
}

// analyseFile counts skip calls (the floor) and records path-guarded ones.
func analyseFile(out *Outcome, root, pkgDir string, pkg *pkgScope, pf parsedFile) {
	ast.Inspect(pf.ast, func(n ast.Node) bool {
		if isSkipCall(n) {
			out.SkipCalls++
		}
		return true
	})

	for _, decl := range pf.ast.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		r := &resolver{root: root, pkgDir: pkgDir, file: pf.rel, pkg: pkg}
		for _, sp := range guardedSkips(fd, r) {
			s := sp.Site
			s.File = pf.rel
			s.Func = fd.Name.Name
			s.Line = pf.fset.Position(sp.pos).Line
			out.Sites = append(out.Sites, s)
		}
	}
}

// siteWithPos carries the token position until the FileSet turns it into a line.
type siteWithPos struct {
	Site
	pos token.Pos
}

// guardedSkips walks one function and returns each skip whose control depends
// on a filesystem probe.
//
// Two shapes occur in this repo and both are handled: the probe inline in the
// if's init (`if _, err := os.Stat(p); os.IsNotExist(err) { t.Skip() }`), and
// the probe on a preceding statement (`data, err := os.ReadFile(p)` followed by
// `if err != nil { t.Skip() }`).
func guardedSkips(fd *ast.FuncDecl, r *resolver) []siteWithPos {
	var sites []siteWithPos
	probes := map[string]ast.Expr{} // error variable -> the path it probed

	var walk func(stmts []ast.Stmt)
	walk = func(stmts []ast.Stmt) {
		for _, st := range stmts {
			switch s := st.(type) {
			case *ast.AssignStmt:
				r.bindAssign(s)
				if name, pathExpr, ok := probeBinding(s); ok {
					probes[name] = pathExpr
				}
			case *ast.DeclStmt:
				r.bindDecl(s)
			case *ast.IfStmt:
				local := probes
				if init, ok := s.Init.(*ast.AssignStmt); ok {
					r.bindAssign(init)
					if name, pathExpr, ok := probeBinding(init); ok {
						local = make(map[string]ast.Expr, len(probes)+1)
						for k, v := range probes {
							local[k] = v
						}
						local[name] = pathExpr
					}
				}
				if pathExpr, ok := notExistGuard(s.Cond, local); ok && bodyHasSkip(s.Body) {
					sites = append(sites, r.newSite(pathExpr, skipPos(s.Body)))
				}
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
			case *ast.BlockStmt:
				walk(s.List)
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
			case *ast.CaseClause:
				walk(s.Body)
			}
		}
	}
	walk(fd.Body.List)
	return sites
}

// newSite resolves the guard's path expression into a classified site.
func (r *resolver) newSite(pathExpr ast.Expr, pos token.Pos) siteWithPos {
	s := siteWithPos{pos: pos}
	s.Expr = r.describe(pathExpr)
	raw, kind := r.resolve(pathExpr, r.file, 0)
	switch kind {
	case kindExternal:
		s.Verdict = External
		return s
	case kindUnresolved:
		s.Verdict = Unresolved
		return s
	}
	var rel string
	if anchored, ok := stripAnchor(raw); ok {
		rel = anchored
	} else {
		rel = filepath.Join(r.pkgDir, raw)
	}
	s.Path = filepath.ToSlash(filepath.Clean(rel))
	if _, err := os.Lstat(filepath.Join(r.root, filepath.FromSlash(s.Path))); err == nil {
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

// notExistGuard reports the guarded path when cond tests a probe's error for
// "the path is not there".
func notExistGuard(cond ast.Expr, probes map[string]ast.Expr) (ast.Expr, bool) {
	switch c := cond.(type) {
	case *ast.BinaryExpr:
		if c.Op != token.NEQ {
			return nil, false
		}
		id, ok := c.X.(*ast.Ident)
		if !ok {
			return nil, false
		}
		if nilIdent, ok := c.Y.(*ast.Ident); !ok || nilIdent.Name != "nil" {
			return nil, false
		}
		p, ok := probes[id.Name]
		return p, ok
	case *ast.CallExpr:
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok || len(c.Args) == 0 {
			return nil, false
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return nil, false
		}
		switch {
		case pkg.Name == "os" && sel.Sel.Name == "IsNotExist",
			pkg.Name == "errors" && sel.Sel.Name == "Is":
		default:
			return nil, false
		}
		id, ok := c.Args[0].(*ast.Ident)
		if !ok {
			return nil, false
		}
		p, ok := probes[id.Name]
		return p, ok
	}
	return nil, false
}

// bodyHasSkip reports whether the block reaches a skip call.
func bodyHasSkip(b *ast.BlockStmt) bool {
	if b == nil {
		return false
	}
	found := false
	ast.Inspect(b, func(n ast.Node) bool {
		if isSkipCall(n) {
			found = true
		}
		return !found
	})
	return found
}

// skipPos is the position of the first skip call in the block.
func skipPos(b *ast.BlockStmt) token.Pos {
	pos := b.Pos()
	ast.Inspect(b, func(n ast.Node) bool {
		if isSkipCall(n) {
			pos = n.Pos()
			return false
		}
		return true
	})
	return pos
}

// isSkipCall reports whether n is `<x>.Skip(...)`, `.Skipf(...)` or
// `.SkipNow()`. The receiver is not type-checked: this package parses without
// type information on purpose (it reads a tree it does not build), and in a
// _test.go file those three selectors are testing.TB's.
func isSkipCall(n ast.Node) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if _, ok := sel.X.(*ast.Ident); !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Skip", "Skipf", "SkipNow":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// path expression resolution
// ---------------------------------------------------------------------------

type resolveKind int

const (
	kindUnresolved resolveKind = iota
	kindResolved
	kindExternal
)

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
	helpers map[string]helperDecl
	consts  map[string]boundExpr
}

// resolver turns a path expression into a string, using only bindings it can
// see. Anything it cannot follow comes back unresolved -- never as a guess.
type resolver struct {
	root   string
	pkgDir string
	file   string
	pkg    *pkgScope
	locals map[string]boundExpr
}

func (r *resolver) bind(name string, e ast.Expr, file string) {
	if name == "" || name == "_" {
		return
	}
	if r.locals == nil {
		r.locals = map[string]boundExpr{}
	}
	r.locals[name] = boundExpr{expr: e, file: file}
}

// bindAssign records `p := "lit"`, `root, err := repoRoot()` and the
// runtime.Caller idiom.
func (r *resolver) bindAssign(a *ast.AssignStmt) {
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
	}
	return "<expr>"
}

// ---------------------------------------------------------------------------
// the pin
// ---------------------------------------------------------------------------

// parsePins reads the declaration file. A missing file is NOT an empty pin:
// the pin is committed, so its absence means it was deleted or the path
// drifted, and reporting a clean run over a pin nobody read is the exact
// failure this gate exists to prevent (the same reasoning B-006 applied to the
// frozen gate-set oracle).
func parsePins(path string) ([]Pin, error) {
	// #nosec G304 -- path is the pin file the gate is configured to read.
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading the pinned declaration %s: %w -- the pin is committed, so its "+
			"absence is a broken setup, not an empty list of tracked skips", path, err)
	}
	defer func() { _ = f.Close() }()

	var pins []Pin
	seen := map[string]int{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		var kept []string
		for _, fld := range strings.Split(line, "\t") {
			if strings.TrimSpace(fld) != "" {
				kept = append(kept, strings.TrimSpace(fld))
			}
		}
		if len(kept) < 4 {
			return nil, fmt.Errorf("%s:%d: cannot parse %q -- expected four TAB-separated fields: "+
				"<test file>\t<func>\t<guarded path>\t<why, naming what covers this instead>", path, n, line)
		}
		p := Pin{
			File:   filepath.ToSlash(kept[0]),
			Func:   kept[1],
			Path:   filepath.ToSlash(filepath.Clean(kept[2])),
			Reason: strings.Join(kept[3:], " "),
			Line:   n,
		}
		if prev, dup := seen[p.Key()]; dup {
			return nil, fmt.Errorf("%s:%d: %s is declared twice (first at line %d) -- one skip, one answer",
				path, n, p.Key(), prev)
		}
		seen[p.Key()] = n
		pins = append(pins, p)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return pins, nil
}
