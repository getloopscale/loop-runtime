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
//   - THE GUARD POSITION REACHES A BODY WHEREVER A NAME CAN CARRY ONE, and this
//     sentence has been earned twice rather than written once. B-189 made it true
//     of package-level names; an audit then measured it false for LOCAL ones the
//     day it was written, and B-193 made it true of those.
//
//     WHAT A LOCAL COST. One binding restored total silence over every
//     UNRESOLVED case: `f := presentFn(); f()` where `presentFn()()` is red,
//     `p := s.probe; p()` where `s.probe()` is UNRESOLVED, `f := probes[k]; f()`
//     where `probes[k]()` is UNRESOLVED. `unreadableFuncValue` accepted an
//     *ast.Ident only when the name was in `pkg.rebound` or `pkg.consts`, and a
//     LOCAL name is in neither, so the fallback whose whole job is to turn a
//     silence into a verdict never ran for the one shape a reader reaches for
//     first when factoring a guard out. The fix is not a fourth index: a name
//     that HOLDS an expression is followed to the expression, and the expression
//     is routed to the same readers the spelling without the binding goes to
//     (guardCall, predicateCondition, heldExpr). Probe-reachability over an
//     expression, not over a name.
//
//     Measured 2026-09-06 with binaries built from this package, nine spellings
//     injected into infra/tests/config/: BEFORE, 12 path-guarded and ONE
//     violation -- and that one was FALSE, an alias accused of a rebinding that
//     never happened. AFTER, 20 path-guarded and nine violations, the alias among
//     them with the ABSENT verdict it always had. Eight silences and one false
//     verdict, for nine spellings each one line from the live guard.
//
//     A twelve-spelling reopening attempt run against the fix caught all twelve;
//     three of them the B-189 build had not: a parenthesised func literal
//     invoked on the spot, a parenthesised local `(f)()`, and a local holding a
//     field read in the condition of a tag-less switch case. All three are
//     pinned in localbinding_test.go.
//
//     THAT ATTEMPT WAS NOT THE LAST WORD, and the sentence above should not be
//     read as coverage. A further audit walked past the same build five more
//     times on 2026-09-06: FOUR were a local whose value an `=` changed from
//     somewhere the walk could not promise had run, and one was a guard that came
//     out of a `range` over the very table whose indexed spelling was already red.
//     Both are the round described under "A LOCAL IS FOLLOWED ONLY WHERE
//     STATEMENT ORDER DECIDES IT" below. A reopening attempt measures what it
//     tried, never what it did not.
//
//     So: a call to a function declared in this package, a method whose name is
//     unambiguous in it, and -- since B-189 -- a name bound to a func VALUE: a package alias
//     `var present = cliSrcPresent`, a package `var f = func() bool {...}`, a
//     local `check := cliPresent`, a method value `p := h.Present`, a struct
//     field read from the composite literal the caller's own scope bound to the
//     receiver, an entry of a table of predicates whose key is a literal
//     (`probes["cli"]()`), and a func value the CALLER hands a helper as an
//     argument (`runWith(t, cliPresent)`, reported at the call, by the same rule
//     every delegated finding follows). `pkgScope.callee` used to resolve an
//     identifier only against FuncDecls, so a name bound to a func value was not
//     found, not read and not reported; an audit walked six such spellings past
//     on 2026-09-05, each one SILENT rather than UNRESOLVED, each a one-line
//     refactor from the live `if !cliSrcPresent() { t.Skip() }` that opened
//     B-183, B-186 and B-188. Measured with a binary built from this package,
//     injected into infra/tests/config/: 708 files, 144 skip calls, 11
//     path-guarded, PASS, exit 0, with a dead t.Fatal under each of the six.
//     The last three spellings above were measured walking past the FIRST fix
//     for the other six, in the same silence, on the same day. A fourth round of
//     the same audit then walked past THIS fix eight more times, which B-193
//     closed.
//
//     PUNCTUATION AND A NAMED TYPE were the two smaller halves of B-193, and both
//     are read now. Parentheses are stripped before routing, so `(cliPresent)()`,
//     `(f)()` and `(func() bool {...})()` reach the reader the unparenthesised
//     spelling reaches; a func literal invoked on the spot is read as the
//     predicate it is. And a struct field declared through a NAMED func type
//     (`type predicate func() bool`, then `probe predicate`) is indexed like one
//     declared with the literal type -- matching only *ast.FuncType left it out
//     of funcFields entirely, so the bar the doc said such a field has to clear
//     could be MET and the guard still walked past. That was a property this
//     comment claimed and the code did not have, which is the worst kind to
//     leave: a reader who trusts it stops looking.
//
//     `pkg.rebound` USED TO ACCUSE A REBINDING THAT DID NOT HAPPEN. It was set by
//     ANY `=` to an identifier anywhere in the package, locals included, while
//     `pkgScope`'s own doc called it "the package-level names that get a second
//     value after their declaration" -- so a package alias nobody ever reassigned
//     was downgraded to UNRESOLVED because an unrelated function shadowed its
//     name, under a printed sentence stating a swap that never occurred. It is
//     filled by indexRebindings now, which carries Go's own block scopes in
//     declaration order: an `=` to a name the enclosing body declared rebinds the
//     LOCAL and is not this index's business, and an `=` to any other identifier
//     is the package-level swap B-189 refused to read past. The refusal that
//     survives a name it simply could not follow says THAT instead, because a
//     reason may not assert more than the check behind it (B-193).
//
//     WHAT A FUNC VALUE STILL COSTS, and it costs a verdict rather than a
//     silence. Three shapes are UNRESOLVED and red:
//
//     A FIELD whose value the caller's scope does not spell -- `s.probe()` where
//     `s` came from a constructor. Which value runs is dataflow, and this gate
//     has none; saying so is the answer, and picking one of the package's
//     assignments to that field would be the guess.
//
//     A TABLE ENTRY whose key this gate cannot read -- `probes[k]()`.
//
//     A NAME REBOUND after its declaration -- `var present = alwaysTrue` swapped
//     in an init() to `cliSrcPresent`. The initializer is no longer the answer,
//     and reading it would be worse than silence: a confident verdict about a
//     function that never ran.
//
//     ALL THREE SURVIVE A LOCAL BINDING. `p := s.probe; p()` and `f := probes[k];
//     f()` cost the same verdict the spelling without the binding costs, which is
//     the whole of B-193's first root cause: before it, the binding bought
//     silence for one line of ordinary refactoring.
//
//     A LOCAL IS FOLLOWED ONLY WHERE STATEMENT ORDER DECIDES IT, which is the
//     second round of B-193 and the one correction here that was not a silence.
//     This walker is textual: it binds names in the order it meets them, and the
//     last `=` it passed won. That is right where every statement between the
//     declaration and the guard certainly runs -- `f := alwaysTrue; f =
//     cliPresent; if !f() {...}` reads cliPresent, and a test pins it. It is a
//     GUESS where the assignment sits somewhere that may not have run: an if arm,
//     a loop or switch body, or a function literal that `defer`, `t.Cleanup`, `go`
//     or a helper runs later or never.
//
//     Measured 2026-09-06 with a binary built from this package, in
//     infra/tests/config/: THREE such spellings were SILENT at exit 0 -- a
//     `defer` closure, a `t.Cleanup` closure and a plain if arm, each assigning a
//     value that touches no filesystem, so the name stopped clearing the bar the
//     guard had cleared one line above -- and a FOURTH was worse than silent.
//     `f := cliPresent; if cond { f = otherPresent }; if !f() {...}` reported
//     ABSENT naming otherPresent's PATH, chosen for no better reason than being
//     the last assignment the walk went past. That is the confident verdict about
//     a function that never ran which this comment refuses three paragraphs up for
//     package-level names, done by the locals map.
//
//     So the doubt is recorded rather than resolved: a name an `=` reaches from a
//     branch or a closure is UNRESOLVED, over the same bar every unreadable guard
//     clears -- something bound under that name has to touch the filesystem, so
//     `dialFn := dialCluster; if c { dialFn = alsoDial }` stays out. Branch
//     awareness is not on offer here and a wrong path stated confidently is worse
//     than saying so.
//
//     THE SAME DOUBT ON A PATH LOCAL was the door the func-value fix left open,
//     and a reopening attempt against that fix walked straight through it:
//     `p := cliDir; if c { p = otherDir }; os.Stat(p)` reported ABSENT naming
//     otherDir, and the `defer` spelling reported it for an assignment that had
//     provably NOT run when os.Stat read p -- not a guess between two candidates
//     but the one answer it could not be. resolve() carries the same refusal now.
//
//     A GUARD THAT COMES OUT OF A RANGE is the table case wearing other
//     punctuation. `probes[k]()` with a key this gate cannot read is UNRESOLVED
//     and red, while `for _, f := range probes { if !f() {...} }` over THE SAME
//     map was silent. The range value is bound to the entry it is -- an index this
//     gate cannot read -- so both spellings reach one reader and one bar, rather
//     than two code paths whose gap is what this item keeps reopening on.
//
//     ONE SHAPE OF THAT FOLLOWING IS DECLARED, NOT DONE: a func value the CALLER
//     handed over, which the helper then calls WITH ARGUMENTS. The value has to
//     be read in the caller's scope and the arguments were written in the
//     helper's, and reading them over there answers about names that frame never
//     bound -- where both frames spell one name differently, that is a confident
//     verdict about the wrong file. So this exact shape stays where B-189 left
//     it rather than being traded for a possible wrong path; the zero-argument
//     spelling, which is what a `func() bool` predicate uses, is read. Both have
//     tests.
//
//     All three share one bar, the bar every unreadable guard here has to clear
//     before it costs a verdict: something the package binds under that name has
//     to touch the filesystem. A table of cluster predicates, or a field nothing
//     fills with a probe, stays as much none of this gate's business as
//     `if !dialCluster() { t.Skip() }` is -- and a LOCAL holding one of those
//     stays out too. `dialFn := dialCluster; dialFn()` is the control the local
//     work was measured against, and it has a test of its own.
//
//     `presentFn()()` -- calling the value another call returned -- is read only
//     when the producer resolves to a same-package body whose WHOLE body is one
//     return, and that return's first result names a func body by the same
//     rules. One unconditional return is knowing where the value came from; a
//     second return is a guess about which one ran. Past that line the guard is
//     UNRESOLVED and red. The bar for asking at all is the one every unreadable
//     guard here has to clear -- the producer must reach a filesystem probe --
//     so a factory returning a cluster predicate stays none of this gate's
//     business.
//
//     What remains genuinely out of reach is a BOOL value: `if !h.havePath
//     { t.Skip() }` over a package-level bool has no body to read at all.
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
		helpers:    map[string]helperDecl{},
		funcs:      map[string]helperDecl{},
		methods:    map[string][]helperDecl{},
		imports:    map[string]map[string]bool{},
		consts:     map[string]boundExpr{},
		funcTypes:  map[string]bool{},
		funcFields: map[string]bool{},
		boundFuncs: map[string][]boundExpr{},
		rebound:    map[string]bool{},
	}
	// Named func types first, across every file: a field spelled `probe
	// predicate` cannot be recognised as func-typed until `type predicate
	// func() bool` has been read, and the two may sit in different files of the
	// same package.
	for _, pf := range files {
		indexFuncTypes(pkg, pf)
	}
	for _, pf := range files {
		pkg.imports[pf.rel] = importNames(pf.ast)
		indexFuncFields(pkg, pf)
		indexRebindings(pkg, pf)
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

// funcFieldReachesProbe reports whether this package declares a func-typed field
// with this name AND ever puts a filesystem-touching func value in it. It is the
// bar that keeps an unreadable field from becoming an opinion about conditions
// this gate has none about: `if !c.ready() { t.Skip() }` over a field nothing
// fills with a probe stays none of its business, exactly as
// `if !dialCluster() { t.Skip() }` does.
func (p *pkgScope) funcFieldReachesProbe(name string) bool {
	return p.funcFields[name] && p.nameReachesProbe(name)
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
