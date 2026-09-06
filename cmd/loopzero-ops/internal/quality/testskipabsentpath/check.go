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
//     guard. It does NOT detect a bare unconditional `t.Skip(...)` with no
//     guard: that shape is at least honest about being unconditional, and
//     classifying WHY a bare skip exists means reading its prose.
//
//     Its damage -- dead assertions underneath -- is a different defect and now
//     has its own gate: `test-skip-unconditional` (B-184) fails a bare skip that
//     carries statements below it, and leaves one that carries nothing alone.
//     The two partition the class by GUARD POSITION and neither reads the
//     other's half: a skip inside an if, a for, a switch case or a select clause
//     is this gate's subject, and a skip that is a direct statement of a function
//     body is that one's.
//
//     This SCOPE used to name two live instances here and call them tracked.
//     Tracked is not fixed: both had asserted nothing since T1.4, and when B-184
//     revived them one turned out to be hiding a live regression -- the org
//     hardcode B-069 removed had moved into tenant-operator/main.go and stayed.
//     The same measurement found SIX more sites the item had not counted, all in
//     t.Run closures.
//
//   - WHERE THE SKIP SITS was the other half of the class, and it was undeclared
//     until an audit walked five spellings past this gate on 2026-09-05. Every
//     one of them is read now (B-186 closed the first, B-188 the rest):
//     function literals (`t.Run("x", func(t *testing.T){ ... })`, `func(){}()`,
//     and a literal bound to a local), `defer` and `go`, `select` and
//     type-switch clauses, the CONDITION of a tag-less `switch` case, a skip one
//     same-package call away from the arm that fires it, a skip handed to a
//     helper as an argument (`skipUnless(t, cliPresent(), "...")`, which carries
//     no `t.Skip` at the site at all), and a receiver of any depth --
//     `h.t.Skip()` used to be invisible to the COUNTER as well as to the scan,
//     which is worse than a missed site: it weakens the floor that exists to
//     notice a dead detector. Measured on 2026-09-05 with a binary built from
//     this package, the three spellings injected into infra/tests/config/: the
//     skip count moved 146 -> 148 for three added skips, and the gate said PASS.
//
//     WHERE A DELEGATED FINDING IS REPORTED follows one rule: at the line whose
//     author has to change it. A guard the CALLER hands to a helper is reported
//     at the call; a guard the helper writes for itself is reported at the
//     helper, once, and not again at every caller. Reporting it at both turned a
//     single guarded helper with two callers into three violations naming three
//     functions -- and three pin lines for one line of code -- measured while
//     building this (2026-09-05). The exception is a helper in a NON-test file of
//     the same package, which this scan never walks in its own right: there the
//     call site is the only place the finding can appear, and the alternative is
//     not a duplicate but a silence.
//
//     THE SKIP'S POSITION IS STILL NOT UNLIMITED, and these are the ways back
//     in, measured rather than assumed:
//
//     A skip reached through a FUNCTION VALUE -- `skip := t.Skip` then `skip()`
//     -- is neither a site nor counted. `t.Skip` there is a selector that is not
//     called and `skip()` is a call that is not a selector, so neither half
//     looks like a skip without following the binding. It is the sharpest hole
//     left and it is declared, not measured shut.
//
//     A skip carried by a helper in ANOTHER PACKAGE is not read: this gate's
//     scope is one directory at a time, and `helpers.SkipUnlessCLI(t)` is a body
//     it never parsed.
//
//     Following a skip through helpers is bounded at maxDelegatedSkipDepth call
//     frames, and past the bound the walk is SILENT rather than UNRESOLVED --
//     the one place in this gate where a bound costs a silence. The reason is
//     that at the bound it is not yet known whether a guard exists at all, and
//     reporting every deeply-nested skip helper as unreadable would bury the
//     real ones. A chain that long is declared, not defended.
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
//   - What is still not a site is the GUARD POSITION itself, and the line is
//     narrower than an earlier draft of this comment drew it. The guard has to be
//     a call to a name DECLARED as a function in this package, or to a method
//     whose name is unambiguous in it. Everything else is invisible:
//
//   - a BOOL value -- `if !h.havePath { t.Skip() }`, a package-level bool.
//     There is no body to read, so this one is honestly out of reach.
//
//   - a FUNC value, which is NOT, and this comment used to imply it was
//     covered by saying the guard may be "a local bound to one". An audit
//     measured six spellings walking past on 2026-09-05, each one silent
//     rather than UNRESOLVED: a package alias `var present = cliSrcPresent`,
//     a package `var f = func() bool {...}`, a local `check := cliPresent`, a
//     method value `p := h.Present`, a struct field holding a func, and
//     `presentFn()()`. `pkgScope.callee` resolves an identifier only against
//     FuncDecls, so a name bound to a func value is not found and not
//     reported. Each is the same one-line refactor away from the live
//     `if !cliSrcPresent() { t.Skip() }` that opened B-183, B-186 and B-188.
//     Tracked as B-189.
//
//     A METHOD is read now (B-188), and reading one has two costs stated
//     plainly. Without type information the receiver's type is unknown, so the
//     method is found by NAME: two types declaring the same name make it
//     AMBIGUOUS, and an ambiguous name whose bodies touch the filesystem is
//     UNRESOLVED and red rather than a pass over a body nobody chose. And a path
//     that lives in the receiver's own fields -- `os.Stat(p.dir)` -- is
//     UNRESOLVED for the same reason any unreadable expression is: there is no
//     value here to read it from. A `pkg.Present()` whose `pkg` is an import
//     name in that file is never read as this package's method, which is the
//     guess that would have been worse than the silence it replaces.
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
		methods: map[string][]helperDecl{},
		imports: map[string]map[string]bool{},
		consts:  map[string]boundExpr{},
	}
	for _, pf := range files {
		pkg.imports[pf.rel] = importNames(pf.ast)
		for _, decl := range pf.ast.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok {
				if fd.Recv == nil {
					pkg.funcs[fd.Name.Name] = helperDecl{decl: fd, file: pf.rel}
					if fd.Type.Params.NumFields() == 0 {
						pkg.helpers[fd.Name.Name] = helperDecl{decl: fd, file: pf.rel}
					}
				} else {
					pkg.methods[fd.Name.Name] = append(pkg.methods[fd.Name.Name],
						helperDecl{decl: fd, file: pf.rel})
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

// importNames is the set of names a file's imports bind, so `pkg.Present()` is
// never read as a method of this package. A dot-import binds no name this
// resolver can see, so it is recorded under "." and never matches a receiver.
func importNames(f *ast.File) map[string]bool {
	names := map[string]bool{}
	for _, spec := range f.Imports {
		if spec.Name != nil {
			names[spec.Name.Name] = true
			continue
		}
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		names[path[strings.LastIndex(path, "/")+1:]] = true
	}
	return names
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
		for _, sp := range guardedSkips(fd, r, map[string]bool{fd.Name.Name: true}, 0) {
			s := sp.Site
			s.File = pf.rel
			s.Func = fd.Name.Name
			s.Line = pf.fset.Position(sp.pos).Line
			out.Sites = append(out.Sites, s)
		}
	}
}

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
	enterLits := func(e ast.Expr) {
		if e == nil {
			return
		}
		ast.Inspect(e, func(n ast.Node) bool {
			if lit, ok := n.(*ast.FuncLit); ok && lit.Body != nil {
				walk(lit.Body.List)
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
				local := probes
				if init, ok := s.Init.(*ast.AssignStmt); ok {
					r.bindAssign(init)
					r.bindBoolPred(init)
					if name, pathExpr, ok := probeBinding(init); ok {
						local = withProbe(probes, name, pathExpr)
					}
				}
				sites = append(sites, r.switchSkips(s, local)...)
				if s.Body != nil {
					walk(s.Body.List)
				}
			case *ast.TypeSwitchStmt:
				// `switch v := x.(type)` never carries a path condition, but its
				// clauses carry statements, and this walker used to match only
				// *ast.SwitchStmt -- so a guard in one was not read at all.
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
	h, name, ok := r.pkg.callee(call, r.file)
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
		if isSkipCall(n) {
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
			return r.methodCondition(sel, c, depth)
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
	if !ok {
		return guardTarget{}, false, false
	}
	return r.predicateBody(name, h, call, depth)
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
	h, _, ok := r.pkg.callee(call, r.file)
	if !ok {
		return guardTarget{}, false, false
	}
	return r.predicateBody(sel.Sel.Name, h, call, depth)
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

// armSkips reports whether an if-arm reaches a skip call -- written there, or
// one same-package call away. `if !cliPresent() { bailOut(t) }` is the same
// guard as the one that spells the skip inline (B-188).
func (r *resolver) armSkips(b *ast.BlockStmt) bool {
	if b == nil {
		return false
	}
	return reachesSkip(b, r.pkg, r.file, map[string]bool{})
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

// isSkipCall reports whether n is `<expr>.Skip(...)`, `.Skipf(...)` or
// `.SkipNow()`. The receiver is not type-checked: this package parses without
// type information on purpose (it reads a tree it does not build), and in a
// _test.go file those three selectors are testing.TB's.
//
// THE RECEIVER IS ANY EXPRESSION, and it had to become so. This required
// `sel.X` to be a bare `*ast.Ident`, so `h.t.Skip("...")` -- a testing.TB
// reached through one field -- was not a skip here. That is worse than a missed
// site: the SkipCalls floor exists to notice a detector that stopped matching,
// and a skip the counter cannot see cannot hold the floor up. Measured
// 2026-09-05 inside the scanned trees: three skips added to
// infra/tests/config/, the count moved 146 -> 148.
func isSkipCall(n ast.Node) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Skip", "Skipf", "SkipNow":
		return true
	}
	return false
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
		if isSkipCall(n) {
			found = true
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		h, name, ok := pkg.callee(call, inFile)
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
func (p *pkgScope) callee(call *ast.CallExpr, inFile string) (helperDecl, string, bool) {
	switch fn := call.Fun.(type) {
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
