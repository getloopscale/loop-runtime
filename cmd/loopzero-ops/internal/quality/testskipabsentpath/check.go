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
// WHAT THIS GATE ANSWERS. For every path-guarded skip under the trees it scans
// (DefaultScanDirs: infra/tests and api), exactly one of four things, named in
// the output:
//
//  1. CONDITIONAL -- the guarded path exists in this tree. The guard is
//     genuinely conditional, the test runs here, nothing to declare.
//  2. ABSENT and UNDECLARED -- the skip is unconditional in fact. FAIL. The
//     fix is to remove the test naming the guard that covers the contract
//     instead (B-169), or to declare it in the pin with a written reason.
//  3. TRACKED -- an absence declared in the pin. Printed on EVERY run, pass or
//     fail, so a tracked absence is never quieter than a fresh one.
//  4. EXTERNAL -- the guard's path is rooted outside this repository: a
//     $HOME/... backup directory, an absolute literal like /usr/bin/helmfile,
//     or a relative path that climbs above the repository root. The gate states
//     that it does not and cannot assert about it, rather than reporting a
//     clean verdict over it -- and it prints the path AS WRITTEN. An earlier
//     version joined an absolute literal onto the repository root and reported
//     "infra/tests/config/usr/bin/helmfile ... ABSENT from this tree", a path
//     that cannot exist as spelled; a gate that fabricates the path it names
//     teaches its readers to distrust the ones it did not (B-183).
//
// The guard's probe may be written inline or moved into a same-package
// predicate function -- `if !cliSrcPresent() { t.Skip() }` is read exactly like
// the os.Stat it wraps (B-183) -- and that predicate may itself delegate to
// another, for as far as the answer stays followable (B-186). Before B-183, a
// one-line refactor moving the probe into a helper made any guard in scope
// invisible here, which is how an independent audit walked the class straight
// back in on 2026-09-05; a second refactor of the same size, moving the probe
// one level further, walked it back in again the same day.
//
// A pin line whose path came BACK also fails: the guard is conditional again
// and the declaration has become stale prose.
//
// WHY IT CANNOT BECOME WHAT IT GUARDS. The failure mode of a scanner is
// finding nothing and calling that clean. Four floors close that:
//
//   - every configured scan directory must exist -- a moved infra/tests/ is an
//     error, not a pass;
//   - every configured scan directory must yield at least one *_test.go file,
//     counted per root so one populous tree cannot cover for a root that
//     drifted;
//   - the scan must find at least one skip call across the roots. If the AST
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
//   - It detects a skip whose GUARD is a filesystem existence probe, written
//     inline or reached through same-package predicates, in either arm of the
//     guard. It does NOT detect a bare
//     unconditional `t.Skip(...)` with no guard: that shape is at least honest
//     about being unconditional, its damage (dead assertions below it) is a
//     different defect, and classifying WHY a bare skip exists means reading its
//     prose. Two live instances are tracked as B-184.
//
//   - WHERE THE SKIP SITS is a limit too, and it was undeclared until an audit
//     walked five spellings past this gate on 2026-09-05. Function literals are
//     read now (`t.Run("x", func(t *testing.T){ ... })` and `func(){ ... }()`),
//     which was the one with a live denominator -- 6 of 146 skip calls in the
//     scanned trees sit inside one, and the ORIGINAL inline spelling this gate
//     was opened for passed both binaries in that position. Still NOT read, and
//     each is a way back in: the condition of a `switch { case !present(): }`
//     clause; a skip moved into a same-package helper (`skipUnless(t, cond)`),
//     which is not a `t.Skip` call at the site at all; and a two-level selector
//     like `h.t.Skip()`, which is not even COUNTED. Tracked as B-188.
//
//   - A guard on a path this repository does not contain -- rooted in an
//     environment variable, spelled as an absolute literal, or climbing above
//     the repository root -- is reported EXTERNAL and not judged. This is a
//     declared limit, not an oversight: such a path is a fact about the machine
//     the suite runs on and not a claim about this repository, and a gate that
//     guessed about $HOME or /usr/bin would fail on one machine and pass on the
//     next. What it must never do is judge it anyway under a path it invented.
//
//   - A predicate the guard calls is READ, not trusted, and the reading has a
//     floor: a function that REACHES one of the probes in fsProbes, but whose
//     answer this resolver cannot follow, is reported UNRESOLVED and FAILS,
//     exactly as an unresolvable inline path does.
//
//     WHAT THAT COSTS, measured rather than promised away. Reachability is
//     TRANSITIVE, so the floor now catches a predicate that touches a file
//     ANYWHERE in its delegation chain, whether or not the file is what the
//     predicate is about. An earlier draft of this comment said the floor
//     "triggers on a filesystem probe and nothing else" and offered
//     `if !clusterReachable() { t.Skip() }` as the shape it leaves alone; an
//     independent audit measured that exact example failing once the predicate
//     loads a kubeconfig before dialling. The claim was false the day it was
//     written.
//
//     It is kept, not narrowed, because the alternative is worse in the
//     direction this gate cares about: the resolver cannot tell a probe that
//     DECIDES the answer from one incidental to computing it, and guessing
//     would put the silence back. A predicate the gate cannot read costs a
//     verdict, and the pin is where a human overrides it with a written
//     reason. `if !dialCluster() { t.Skip() }` over a predicate that touches no
//     file is still left alone.
//
//     "REACHES" IS TRANSITIVE, and it was made so by measurement. It used to
//     mean the predicate's OWN body, and a predicate that DELEGATES --
//     `present() { return dirExists(p) }` -- reaches no probe of its own, so
//     the bail fired before its answer was ever read: invisible, and SILENTLY
//     so rather than UNRESOLVED. An independent audit walked exactly that on
//     2026-09-05 and the gate printed PASS (B-186). Reachability now follows
//     same-package calls under a visited set, so it reads every body the
//     predicate can reach, exactly once, and a cycle ends the walk instead of
//     truncating its answer.
//
//     THE DEPTH IS BOUNDED AND THE BOUND IS NOT SILENT. Following the answer
//     costs maxResolveDepth (24) expression hops, a delegation being one of
//     them. Past the bound -- a chain longer than that, or a cycle in the
//     answer -- the predicate is UNRESOLVED and the gate FAILS. There is no
//     depth at which this gate goes quiet: it either reads the answer or says
//     it could not read it. Unbounded recursion is not the alternative -- a
//     gate that hangs on a cyclic predicate is a gate nobody runs.
//
//     A predicate whose FIRST result is a bool is read even when it returns
//     more. `ok, _ := cliSrcPresent()` followed by `if !ok { t.Skip() }` is the
//     same guard spelled with an error result; it used to be rejected before
//     the probe check ran, so the floor never got a chance (B-186).
//
//     BOTH ARMS ARE READ. `if present() { ... } else { t.Skip() }` fires the
//     skip exactly on absence, so it is a site. The opposite polarity -- a skip
//     that fires when the path IS there, in either arm -- remains deliberately
//     not a site, because no missing path can cause it.
//
//   - What is still not a site is the GUARD POSITION itself: a struct field, a
//     package-level bool, a method on a value. The guard has to be a call to a
//     same-package function, or a local bound to one. A probe behind anything
//     else is invisible here -- not measured shut, simply not read.
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

// DefaultScanDirs are the trees this gate is responsible for, repo-relative.
//
// infra/tests is where B-180 found the class. api joined it in B-183, because
// the two permanent cli/ skips in api/internal/contract/cli_consumer_test.go are
// the live instances that opened the item, and a tree the gate does not scan is
// a tree it cannot track: the pin's re-find floor rejects a declaration for a
// file outside the scan, measured 2026-09-05. Scanning all of api/ rather than
// the one package costs nothing measured -- those two are the only path-guarded
// skips in its 504 test files.
var DefaultScanDirs = []string{"infra/tests", "api"}

// DefaultPinFile is the declaration of tracked path-guarded skips. It sits in
// infra/quality/ beside the other pinned contracts this repo keeps for its own
// test governance (test-pyramid-baseline.json, skip-link-baseline.txt).
const DefaultPinFile = "infra/quality/test-skip-absent-path.txt"

// maxResolveDepth bounds the expression resolver against a cyclic binding, and
// bounds how far a guard's answer is followed through predicates that delegate
// to other predicates (B-186). Past it the guard is UNRESOLVED and the gate
// FAILS -- the bound costs a verdict, never a silence.
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
	// ScanDirs overrides DefaultScanDirs (repo-relative).
	ScanDirs []string
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
	// Outside: the path resolved, but to a place this repository does not
	// contain -- an absolute literal, or a climb above the repository root.
	// Reported with the others under EXTERNAL, and never joined onto the repo
	// root: the fabricated path was the whole defect (B-183).
	Outside
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
	// Present, Absent or Outside -- and for Outside it is the path AS WRITTEN,
	// which is the point: nothing is joined onto a root it does not share.
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

	// ScanRoots are the directories that were walked, as resolved.
	ScanRoots []string
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
	scanDirs := cfg.ScanDirs
	if len(scanDirs) == 0 {
		scanDirs = DefaultScanDirs
	}
	pinFile := cfg.PinFile
	if pinFile == "" {
		pinFile = DefaultPinFile
	}

	pins, err := parsePins(filepath.Join(root, pinFile))
	if err != nil {
		return nil, err
	}

	out := &Outcome{}
	for _, dir := range scanDirs {
		scanRoot := filepath.Join(root, dir)
		info, err := os.Stat(scanRoot)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("the scan tree %s is not a directory (%v) -- this gate has no subject, "+
				"which is a broken setup and not a clean repository", scanRoot, err)
		}
		out.ScanRoots = append(out.ScanRoots, scanRoot)
		// The file floor is PER ROOT: a configured tree with no test file in it
		// is a root that drifted, and averaging it away behind the other root's
		// hundreds is how a scan quietly stops covering half its subject.
		before := out.FilesScanned
		if err := scan(out, root, scanRoot); err != nil {
			return nil, err
		}
		if out.FilesScanned == before {
			return nil, fmt.Errorf("%s: %w", scanRoot, errNoTestFiles)
		}
	}
	// The skip floor is GLOBAL: it guards one AST matcher shared by every root,
	// and per-root it would punish the right fix -- deleting a tree's last dead
	// skip would turn the gate red for the deed it exists to encourage.
	if out.SkipCalls == 0 {
		return nil, fmt.Errorf("%s: %w", strings.Join(out.ScanRoots, ", "), errNoSkipCalls)
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
		case Outside:
			// Named as written, never rebased onto the repository root. The
			// defect this replaces printed a path that could not exist as
			// spelled, and a fabricated path is worse than a withheld verdict.
			out.Externals = append(out.Externals, fmt.Sprintf(
				"%s:%d %s -> `%s` resolves to %s, which this repository does not contain; NOT judged "+
					"(whether it is on the machine running the suite is not a claim about this tree)",
				s.File, s.Line, s.Func, s.Expr, s.Path))
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

	// Package-wide binding scope: zero-argument PATH helpers (repoRoot() and
	// friends), every non-method func by name so a guard's predicate can be
	// read, and file-level const/var literals -- from every file in the
	// directory.
	pkg := &pkgScope{
		helpers: map[string]helperDecl{},
		funcs:   map[string]helperDecl{},
		consts:  map[string]boundExpr{},
	}
	for _, pf := range files {
		for _, decl := range pf.ast.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok {
				if fd.Recv == nil {
					pkg.funcs[fd.Name.Name] = helperDecl{decl: fd, file: pf.rel}
					if fd.Type.Params.NumFields() == 0 {
						pkg.helpers[fd.Name.Name] = helperDecl{decl: fd, file: pf.rel}
					}
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
// Four shapes occur and all are handled: the probe inline in the if's init
// (`if _, err := os.Stat(p); os.IsNotExist(err) { t.Skip() }`), the probe on a
// preceding statement (`data, err := os.ReadFile(p)` followed by
// `if err != nil { t.Skip() }`), the probe down inside a same-package predicate
// the guard calls (`if !cliSrcPresent() { t.Skip() }`), and that same predicate
// bound to a local first (`ok, _ := cliSrcPresent()` then `if !ok { ... }`),
// which is the only way a predicate returning `(bool, error)` can be spelled.
func guardedSkips(fd *ast.FuncDecl, r *resolver) []siteWithPos {
	var sites []siteWithPos
	probes := map[string]ast.Expr{} // error variable -> the path it probed

	var walk func(stmts []ast.Stmt)
	walk = func(stmts []ast.Stmt) {
		for _, st := range stmts {
			switch s := st.(type) {
			case *ast.AssignStmt:
				r.bindAssign(s)
				r.bindBoolPred(s)
				if name, pathExpr, ok := probeBinding(s); ok {
					probes[name] = pathExpr
				}
			case *ast.DeclStmt:
				r.bindDecl(s)
			case *ast.IfStmt:
				local := probes
				if init, ok := s.Init.(*ast.AssignStmt); ok {
					r.bindAssign(init)
					r.bindBoolPred(init)
					if name, pathExpr, ok := probeBinding(init); ok {
						local = make(map[string]ast.Expr, len(probes)+1)
						for k, v := range probes {
							local[k] = v
						}
						local[name] = pathExpr
					}
				}
				if tgt, arm, ok := r.skipArm(s, local); ok {
					sites = append(sites, r.newSite(tgt, skipPos(arm)))
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
			case *ast.ExprStmt:
				// `t.Run("name", func(t *testing.T){ ... })` and `func(){ ... }()`.
				//
				// This walker read statements and never entered a function
				// literal, so a guard one closure down was invisible — including
				// the ORIGINAL inline `os.Stat` spelling this gate was opened for
				// (B-180), which passed both binaries inside a t.Run. Measured by
				// an independent audit on 2026-09-05, with a live denominator: 6
				// of 146 skip calls in the scanned trees already sit inside
				// function literals.
				//
				// The probe map is shared with the enclosing body on purpose: a
				// probe taken outside the closure and tested inside it is one
				// guard, and scoping them apart would lose it.
				ast.Inspect(s.X, func(n ast.Node) bool {
					lit, ok := n.(*ast.FuncLit)
					if ok && lit.Body != nil {
						walk(lit.Body.List)
					}
					return true
				})
			}
		}
	}
	walk(fd.Body.List)
	return sites
}

// newSite resolves the guard's path expression into a classified site. The
// expression is resolved in the scope it was WRITTEN in, which for a predicate's
// probe is the predicate's own body and not the test's -- resolving `p` against
// the caller's `p` would answer about a different path with a straight face.
func (r *resolver) newSite(t guardTarget, pos token.Pos) siteWithPos {
	s := siteWithPos{pos: pos}
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
}

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
	case (whenMissing || t.unreadable) && bodyHasSkip(s.Body):
		return t, s.Body, true
	case (!whenMissing || t.unreadable) && bodyHasSkip(elseBlock):
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
		// `ok, _ := cliSrcPresent()` then `if !ok { t.Skip() }`. The local IS
		// the predicate's answer, and this is the only spelling available to a
		// predicate returning (bool, error) -- Go has no other way to use one in
		// a condition (B-186).
		call, bound := r.boolPreds[c.Name]
		if !bound {
			return guardTarget{}, false, false
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok {
			return guardTarget{}, false, false
		}
		return r.predicateCondition(id.Name, call, depth+1)
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
		if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
			if len(c.Args) == 0 {
				return guardTarget{}, false, false
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return guardTarget{}, false, false
			}
			switch {
			case pkg.Name == "os" && sel.Sel.Name == "IsNotExist",
				pkg.Name == "errors" && sel.Sel.Name == "Is":
			default:
				return guardTarget{}, false, false
			}
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
		id, ok := c.Fun.(*ast.Ident)
		if !ok {
			return guardTarget{}, false, false
		}
		return r.predicateCondition(id.Name, c, depth)
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
func (r *resolver) predicateCondition(name string, call *ast.CallExpr, depth int) (guardTarget, bool, bool) {
	h, ok := r.pkg.funcs[name]
	if !ok || h.decl.Body == nil || !returnsBoolFirst(h.decl) {
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
		h, ok := pkg.funcs[id.Name]
		if !ok || h.decl.Body == nil {
			return true
		}
		seen[id.Name] = true
		if reachesFilesystemProbe(h.decl.Body, pkg, seen) {
			found = true
			return false
		}
		return true
	})
	return found
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
	// helpers are the zero-argument funcs a PATH expression may call.
	helpers map[string]helperDecl
	// funcs is every non-method func in the package, by name. A guard's
	// predicate is looked up here rather than in helpers because a presence
	// predicate may take the path as an argument.
	funcs  map[string]helperDecl
	consts map[string]boundExpr
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
	id, ok := call.Fun.(*ast.Ident)
	if !ok {
		return
	}
	h, ok := r.pkg.funcs[id.Name]
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
