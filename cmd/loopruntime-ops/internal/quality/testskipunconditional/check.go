// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package testskipunconditional implements the `test-skip-unconditional` gate.
//
// PROBLEM (B-184). A test opens with a BARE t.Skip -- no guard, no condition --
// and carries live-looking code underneath it:
//
//	func TestB69GiteaOrgConfigurable(t *testing.T) {
//	        t.Skip("monolith operators/cmd/main.go deleted T1.4 -- covered per-binary")
//	        data, err := os.ReadFile("../../../operators/cmd/main.go")
//	        if err != nil {
//	                t.Fatalf("read operators/cmd/main.go: %v", err)
//	        }
//	        ... six statements of regression assertion ...
//	}
//
// A skip marks the WHOLE test skipped -- it is not a branch, it aborts the
// function -- so nothing below it has run since the line was added, while the
// package goes on printing `ok`. That is the damage B-169 and B-180 removed in
// the path-guarded spelling. This is the same damage with the guard deleted,
// and the sibling gate deliberately does not see it: `test-skip-absent-path`
// looks for a filesystem probe in a guard, and here there is no guard to read.
// Its SCOPE said so, and named two live instances as B-184; this gate is what
// closed them, and it found six more the item had not measured.
//
// WHAT THIS GATE ANSWERS. For every *_test.go under DefaultScanDirs it finds
// each skip that is a DIRECT statement of a function body -- a test function's
// or a function literal's -- which makes it unconditional within that body, and
// asks one question: is there a statement after it?
//
//   - No statement after it. The skip is honest. It is counted, printed in the
//     summary, and nothing is claimed about WHY it exists -- classifying a
//     skip's reason means reading its prose, and this gate does not pretend to.
//   - A statement after it, and the site is declared in the pin with a written
//     reason: TRACKED. Printed on EVERY run, pass or fail, so a declared
//     absence is never quieter than a fresh one.
//   - A statement after it, undeclared: FAIL. The fix is the B-169/B-180 bar --
//     either the assertion comes back and asserts something that is in this
//     tree, or the test goes, with the reason written, naming what covers the
//     contract in its place.
//
// THERE IS NO AUTOMATIC EXEMPTION, and that is a deliberate choice against a
// tempting one. The measured tree holds a helper that ends
// `t.Skip(...)` / `return nil, nil`, where the trailing return exists only to
// satisfy the compiler and reads as nothing at all. A rule that quietly forgave
// "trailing statements that are only trivial returns" would have been three
// lines and would have cost the property below: the pin would then be EMPTY,
// and an empty pin re-find floor protects nothing. Declaring that one site in
// the pin costs a line of prose and buys a live anchor.
//
// WHY IT CANNOT BECOME WHAT IT GUARDS. The failure mode of a scanner is finding
// nothing and calling that clean. A detector that stops recognising this shape
// must go RED, not green. Five floors, none of which is ever reported as a
// pass:
//
//   - every configured scan directory must exist -- a moved infra/tests/ is an
//     error, not a pass;
//   - every configured scan directory must yield at least one *_test.go file,
//     counted PER ROOT so one populous tree cannot cover for a root that
//     drifted;
//   - the scan must find at least one skip call across the roots. If the AST
//     walk stops matching the shape of a skip at all, that is a broken
//     detector, not a clean repository;
//   - the scan must find at least one UNCONDITIONAL skip across the roots. This
//     floor is this gate's own, and it exists because the floor above is not
//     enough: this gate matches a strictly narrower shape than "a skip call",
//     so a working skip matcher proves nothing about the function-body-level
//     test that sits on top of it. The live denominator is 12 unconditional
//     skips across the scanned trees, of 138 skip calls in 707 test files
//     (measured 2026-09-05 from the gate's own output, after this item's eight
//     sites were resolved), and exactly one of the 12 has anything underneath;
//   - EVERY pin line must be re-found by this run's scan. The pin holds live
//     anchors, and the anchor is the whole shape: file, function, AND the fact
//     that statements follow the skip. If the trailing-statement test breaks --
//     the one floor a skip-counting check cannot reach -- the pinned site stops
//     being a site, its line goes unmatched, and the gate goes red naming it. A
//     declaration nobody re-finds is the detector's own obituary.
//
// The first four are SETUP BROKEN (exit 5) because the gate could not run; the
// pin re-find is a FAIL (exit 1) because the gate ran and found a declaration
// that protects nothing. The split is the sibling gate's, and it is deliberate:
// a reader auditing the floors by exit code should not go looking for a fifth 5.
//
// SCOPE, stated so it is not mistaken for more.
//
//   - UNCONDITIONAL here means "a direct statement of a function body". A skip
//     inside an if, a for, a switch case or a select clause is conditional in
//     form and is NOT this gate's subject; whether such a guard is conditional
//     in FACT is exactly what `test-skip-absent-path` answers. The two gates
//     partition the class between them and neither reads the other's half.
//
//   - It does not judge WHY a bare skip exists, and it never fails one that has
//     nothing underneath it. `t.Skip("requires cloud cluster + 24h soak")` as
//     the last statement of a test is a test that declares it does not run
//     here, which is honest; whether the repository should carry it is a
//     question about the test, not about this shape. The defect this gate
//     names is narrower and entirely mechanical: code that reads as an
//     assertion and cannot execute.
//
//   - WHERE THE SKIP SITS is read for two positions and not for others.
//     Function declarations and function literals are both read, because the
//     literal is where t.Run subtests live. A skip through any receiver
//     (`h.t.Skip()`) is a skip: the definition is skipshape.IsSkipCall, shared
//     with the sibling gate so the two cannot disagree about what a skip is.
//     Still NOT read: a skip moved into a same-package helper (`skipUnless(t)`),
//     which is not a skip call at the site at all.
//
//   - It reads a tree it does not build: parsing only, no type information. In
//     a _test.go file `x.Skip(...)` is testing.TB's, and the receiver is not
//     checked. A local variable named `t` that is not a *testing.T would be
//     counted; no such thing exists in the scanned trees, and the alternative
//     (loading type information for 650 test files) buys nothing this gate
//     would act on differently.
package testskipunconditional

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
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/skipshape"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

// DefaultScanDirs are the trees this gate governs; see skipshape.
var DefaultScanDirs = skipshape.DefaultScanDirs

// DefaultPinFile is the declaration of tracked unconditional skips. It sits in
// infra/quality/ beside the other pinned contracts this repo keeps for its own
// test governance.
const DefaultPinFile = "infra/quality/test-skip-unconditional.txt"

// Config is the gate's input.
type Config struct {
	RepoRoot string
	ScanDirs []string
	PinFile  string
}

// Outcome is what the gate found. Every slice is sorted, so the output of two
// runs over the same tree is byte-identical.
type Outcome struct {
	Pass         bool
	FilesScanned int
	ScanRoots    []string

	// SkipCalls counts every skip-shaped call anywhere in the scanned files.
	// It is the denominator of the third floor.
	SkipCalls int

	// Unconditional lists every skip that is a direct statement of a function
	// body, whether or not anything follows it. It is the denominator of the
	// fourth floor.
	Unconditional []string

	// Sites are the unconditional skips that have at least one statement after
	// them -- the shape this gate exists for.
	Sites []string

	// Tracked are Sites declared in the pin, printed on every run.
	Tracked []string

	Violations []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// site is one unconditional skip, with what follows it.
type site struct {
	File     string // repo-relative
	Line     int
	Func     string // enclosing test function (a literal is attributed to it)
	InLit    bool   // the skip sits in a function literal inside Func
	After    int    // statements following the skip in the same body
	SkipText string // first string-literal argument, truncated, for the message
}

func (s site) key() string { return s.File + "\t" + s.Func }

func (s site) String() string {
	where := "func body"
	if s.InLit {
		where = "func literal"
	}
	return fmt.Sprintf("%s:%d: %s (%s) skips unconditionally, and %d statement(s) follow: %s",
		s.File, s.Line, s.Func, where, s.After, s.SkipText)
}

// pinLine is one declared site.
type pinLine struct {
	File string
	Func string
	Why  string
	Raw  int // 1-indexed line number in the pin, for error messages
}

// Check runs the gate. A non-nil error is a BROKEN SETUP -- the gate could not
// run -- and is never a pass. A false Outcome.Pass is a contract violation.
func Check(cfg Config) (Outcome, error) {
	var o Outcome
	if len(cfg.ScanDirs) == 0 {
		return o, errors.New("no scan directories configured")
	}

	var all []site
	perRoot := map[string]int{}
	for _, rel := range cfg.ScanDirs {
		abs := filepath.Join(cfg.RepoRoot, rel)
		info, err := os.Stat(abs)
		if err != nil {
			return o, fmt.Errorf("scan directory %s: %w", rel, err)
		}
		if !info.IsDir() {
			return o, fmt.Errorf("scan directory %s is not a directory", rel)
		}
		o.ScanRoots = append(o.ScanRoots, rel)

		err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), "_test.go") {
				return nil
			}
			relFile, err := filepath.Rel(cfg.RepoRoot, p)
			if err != nil {
				return err
			}
			relFile = filepath.ToSlash(relFile)
			perRoot[rel]++
			o.FilesScanned++

			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, p, nil, 0)
			if err != nil {
				return fmt.Errorf("parse %s: %w", relFile, err)
			}
			calls, sites := scanFile(fset, relFile, f)
			o.SkipCalls += calls
			all = append(all, sites...)
			return nil
		})
		if err != nil {
			return o, err
		}
		if perRoot[rel] == 0 {
			return o, fmt.Errorf("scan directory %s holds no *_test.go file", rel)
		}
	}

	// --- floors three and four: the detector must have found its subject. ---
	if o.SkipCalls == 0 {
		return o, fmt.Errorf("no skip call found in %d scanned file(s) under %s -- "+
			"a repository with no skips at all is possible, a detector that stopped "+
			"matching them is likelier, and this gate cannot tell the difference",
			o.FilesScanned, strings.Join(o.ScanRoots, ", "))
	}
	if len(all) == 0 {
		return o, fmt.Errorf("found %d skip call(s) in %d scanned file(s) but not one "+
			"UNCONDITIONAL skip -- this gate matches a narrower shape than a skip call, "+
			"so a working skip counter proves nothing about the function-body-level test "+
			"on top of it", o.SkipCalls, o.FilesScanned)
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].File != all[j].File {
			return all[i].File < all[j].File
		}
		return all[i].Line < all[j].Line
	})
	for _, s := range all {
		o.Unconditional = append(o.Unconditional, fmt.Sprintf("%s:%d: %s", s.File, s.Line, s.Func))
	}

	pins, err := readPin(filepath.Join(cfg.RepoRoot, cfg.PinFile), cfg.PinFile)
	if err != nil {
		return o, err
	}

	// --- classify ---
	//
	// A pin line addresses a site by file and function, which is the sibling
	// gate's spelling and the only one that survives an edit above it. That
	// spelling is AMBIGUOUS if one function holds two of these sites, and the
	// ambiguity is not survivable: one declaration would silently forgive both,
	// so adding a second dead-assertion skip to an already-declared function
	// would cost nothing. Refuse to run instead of choosing.
	perKey := map[string]int{}
	for _, s := range all {
		if s.After > 0 {
			perKey[s.key()]++
		}
	}
	for k, n := range perKey {
		if n < 2 {
			continue
		}
		if _, declared := pins[k]; declared {
			f := strings.SplitN(k, "\t", 2)
			return o, fmt.Errorf("%s declares %s in %s, but this run found %d "+
				"unconditional skips with statements after them in that one function. "+
				"A single line cannot answer for two sites without forgiving the one "+
				"nobody wrote it for", cfg.PinFile, f[1], f[0], n)
		}
	}

	matched := map[string]bool{}
	for _, s := range all {
		if s.After == 0 {
			continue
		}
		o.Sites = append(o.Sites, s.String())
		if p, ok := pins[s.key()]; ok {
			matched[s.key()] = true
			o.Tracked = append(o.Tracked, fmt.Sprintf("%s:%d: %s -- %s", s.File, s.Line, s.Func, p.Why))
			continue
		}
		o.Violations = append(o.Violations, s.String())
	}

	// --- floor five: every pin line must be re-found by this run's scan. ---
	var stale []pinLine
	for k, p := range pins {
		if !matched[k] {
			stale = append(stale, p)
		}
	}
	sort.Slice(stale, func(i, j int) bool { return stale[i].Raw < stale[j].Raw })
	for _, p := range stale {
		o.Violations = append(o.Violations, fmt.Sprintf(
			"%s: pin line %d declares %s in %s, and this run did not re-find it as an "+
				"unconditional skip with statements after it. Either the site was fixed and "+
				"the line is stale prose, or this gate stopped detecting the shape it "+
				"declares -- both need a human, neither is a pass",
			cfg.PinFile, p.Raw, p.Func, p.File))
	}

	sort.Strings(o.Violations)
	o.Pass = len(o.Violations) == 0
	return o, nil
}

// scanFile returns the count of skip-shaped calls in f and the unconditional
// ones among them.
func scanFile(fset *token.FileSet, relFile string, f *ast.File) (int, []site) {
	calls := 0
	ast.Inspect(f, func(n ast.Node) bool {
		if skipshape.IsSkipCall(n) {
			calls++
		}
		return true
	})

	var out []site
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		name := fd.Name.Name
		out = append(out, bodySites(fset, relFile, name, false, fd.Body)...)
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if lit, ok := n.(*ast.FuncLit); ok && lit.Body != nil {
				out = append(out, bodySites(fset, relFile, name, true, lit.Body)...)
			}
			return true
		})
	}
	return calls, out
}

// bodySites finds skips that are DIRECT statements of body -- unconditional
// within it -- and records how many statements follow each.
func bodySites(fset *token.FileSet, relFile, fn string, inLit bool, body *ast.BlockStmt) []site {
	var out []site
	for i, st := range body.List {
		es, ok := st.(*ast.ExprStmt)
		if !ok || !skipshape.IsSkipCall(es.X) {
			continue
		}
		out = append(out, site{
			File:     relFile,
			Line:     fset.Position(es.Pos()).Line,
			Func:     fn,
			InLit:    inLit,
			After:    len(body.List) - i - 1,
			SkipText: skipText(es.X),
		})
	}
	return out
}

// skipText is the skip's first string-literal argument, truncated. It is quoted
// back in the failure so the reader sees the reason WITHOUT opening the file --
// which is usually enough to decide whether the assertion below should come
// back or go.
func skipText(x ast.Expr) string {
	call, ok := x.(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return `""`
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "(non-literal reason)"
	}
	s := lit.Value
	const max = 80
	if len(s) > max {
		return s[:max] + `..."`
	}
	return s
}

// readPin parses the declaration file. Every parse failure is a BROKEN SETUP,
// never a pass: a pin the gate cannot read is a set of declarations it cannot
// honour, and continuing would turn tracked sites into fresh violations or, if
// the file were skipped entirely, fresh violations into silence.
func readPin(abs, rel string) (map[string]pinLine, error) {
	// #nosec G304 -- abs is the pin file the gate is configured to read; same
	// annotation and same reason as testskipabsentpath's parsePins, which reads
	// its own declaration file the same way.
	f, err := os.Open(abs)
	if err != nil {
		return nil, fmt.Errorf("pin file %s: %w", rel, err)
	}
	defer f.Close()

	pins := map[string]pinLine{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			return nil, fmt.Errorf("%s line %d: want 3 TAB-separated fields "+
				"(file, func, why), got %d", rel, n, len(parts))
		}
		p := pinLine{File: strings.TrimSpace(parts[0]), Func: strings.TrimSpace(parts[1]),
			Why: strings.TrimSpace(parts[2]), Raw: n}
		if p.File == "" || p.Func == "" {
			return nil, fmt.Errorf("%s line %d: file and func must not be empty", rel, n)
		}
		if len(p.Why) < 40 {
			return nil, fmt.Errorf("%s line %d: the reason is %d characters. A declaration "+
				"is a claim somebody has to be able to check later; write what covers the "+
				"contract instead, and why the code below the skip still stands", rel, n, len(p.Why))
		}
		k := p.File + "\t" + p.Func
		if _, dup := pins[k]; dup {
			return nil, fmt.Errorf("%s line %d: duplicate declaration for %s in %s",
				rel, n, p.Func, p.File)
		}
		pins[k] = p
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	return pins, nil
}
