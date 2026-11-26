// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package testskipabsentpath

import (
	"fmt"
	"testing"
)

// WHERE THE SKIP SITS, which is the half three audits kept walking through.
//
// B-180 -> B-183 -> B-186 each declared the class closed and each closed a
// spelling of the GUARD. An independent audit on 2026-09-05 reopened it by
// moving the SKIP instead, and three of its five spellings survived B-186:
// the condition of a `switch` case, a skip handed to a same-package helper, and
// a two-level selector `h.t.Skip(...)` that the counting floor could not even
// see. Each is a one-line refactor from a live guard.
//
// Measured before the fix, inside the tree the gate scans (infra/tests/config/),
// with a binary built from this package: the three evasions raised the skip
// count from 146 to 148 -- three skips added, two counted -- and left the gate
// at PASS with 11 path-guarded sites. After: 149 counted, 14 path-guarded,
// exit 1 naming file, function and path for each.

// preamble is a package that declares an absent-path predicate, so each source
// below only has to spell the position under test.
const preamble = `package unit

import (
	"os"
	"testing"
)

const cliSrc = "../../../cli/src"

func cliPresent() bool {
	info, err := os.Stat(cliSrc)
	return err == nil && info.IsDir()
}
`

// treeWith builds a repository whose single test file is preamble + body.
func treeWith(t *testing.T, body string) string {
	t.Helper()
	return tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": preamble + body,
	})
}

// wantOneFailingSite is the shared oracle: exactly one site, and the gate red
// because the path is absent and undeclared.
func wantOneFailingSite(t *testing.T, body string) *Outcome {
	t.Helper()
	o := mustRun(t, treeWith(t, body))
	if len(o.Sites) != 1 {
		t.Fatalf("want exactly one site; got %d: %+v", len(o.Sites), o.Sites)
	}
	if o.Pass {
		t.Fatalf("cli/src is absent and undeclared, so the gate must fail; violations:\n%s",
			joined(o.Violations))
	}
	return o
}

func wantNoSite(t *testing.T, body string) *Outcome {
	t.Helper()
	o := mustRun(t, treeWith(t, body))
	if len(o.Sites) != 0 {
		t.Fatalf("want no site; got %d: %+v", len(o.Sites), o.Sites)
	}
	return o
}

// ---------------------------------------------------------------------------
// the three spellings the 2026-09-05 audit left open
// ---------------------------------------------------------------------------

func TestTheConditionOfASwitchCaseIsAGuard(t *testing.T) {
	// guardedSkips walked a CaseClause's Body and never read its List, so the
	// skip was seen and the guard that fires it was not.
	wantOneFailingSite(t, `
func TestOuter(t *testing.T) {
	switch {
	case !cliPresent():
		t.Skip("cli/ is not here")
	}
	t.Fatal("dead assertion")
}
`)
}

func TestASkipHandedToASamePackageHelperIsASite(t *testing.T) {
	// There is no t.Skip at the call site at all. The damage is identical: the
	// whole test skips and everything under it is dead.
	o := wantOneFailingSite(t, `
func skipUnless(t *testing.T, ok bool, msg string) {
	if !ok {
		t.Skip(msg)
	}
}

func TestOuter(t *testing.T) {
	skipUnless(t, cliPresent(), "cli/ is not here")
	t.Fatal("dead assertion")
}
`)
	// The site is reported at the CALL, in the test that skips -- not in the
	// helper, which is not the line anybody has to change.
	if o.Sites[0].Func != "TestOuter" {
		t.Fatalf("the site belongs to the test that skips, not the helper; got %q", o.Sites[0].Func)
	}
}

func TestATwoLevelSelectorIsCounted(t *testing.T) {
	// The counting floor is the net that catches a dead detector: "found no
	// skip call at all" is a broken setup, not a clean tree. isSkipCall
	// required the receiver to be a bare identifier, so `h.t.Skip(...)` was not
	// a skip here -- it escaped the floor as well as the scan. Measured in the
	// real trees: 3 skips added, the count moved 146 -> 148.
	o := mustRun(t, treeWith(t, `
type harness struct{ t *testing.T }

func TestOuter(t *testing.T) {
	h := &harness{t: t}
	h.t.Skip("bare, through one field")
}
`))
	if o.SkipCalls != 1 {
		t.Fatalf("a skip reached through a field is still a skip the floor must count; got %d", o.SkipCalls)
	}
	if !o.Pass {
		t.Fatalf("a BARE skip is B-184's class and not a site here; violations:\n%s", joined(o.Violations))
	}
}

func TestATwoLevelSelectorIsASite(t *testing.T) {
	wantOneFailingSite(t, `
type harness struct{ t *testing.T }

func TestOuter(t *testing.T) {
	h := &harness{t: t}
	if !cliPresent() {
		h.t.Skip("cli/ is not here")
	}
	t.Fatal("dead assertion")
}
`)
}

// ---------------------------------------------------------------------------
// the reopen attempt: positions nobody had tried
// ---------------------------------------------------------------------------

func TestASkipInsideADeferIsASite(t *testing.T) {
	wantOneFailingSite(t, `
func TestOuter(t *testing.T) {
	defer func() {
		if !cliPresent() {
			t.Skip("cli/ is not here")
		}
	}()
	t.Log("body")
}
`)
}

func TestASkipInsideASelectCaseIsASite(t *testing.T) {
	wantOneFailingSite(t, `
func TestOuter(t *testing.T) {
	ch := make(chan struct{}, 1)
	select {
	case <-ch:
		if !cliPresent() {
			t.Skip("cli/ is not here")
		}
	}
	t.Fatal("dead assertion")
}
`)
}

func TestASkipInsideATypeSwitchIsASite(t *testing.T) {
	// The walker matched *ast.SwitchStmt and not *ast.TypeSwitchStmt, so the
	// clause bodies of a type switch were not walked at all.
	wantOneFailingSite(t, `
func TestOuter(t *testing.T) {
	var v any = "x"
	switch v.(type) {
	case string:
		if !cliPresent() {
			t.Skip("cli/ is not here")
		}
	}
	t.Fatal("dead assertion")
}
`)
}

func TestTheDefaultArmOfASwitchIsRead(t *testing.T) {
	// `case cliPresent(): ... default: t.Skip()` is the else arm of one guard,
	// read for the same reason skipArm reads both arms of an if (B-186).
	wantOneFailingSite(t, `
func TestOuter(t *testing.T) {
	switch {
	case cliPresent():
		t.Log("runs when the path is here")
	default:
		t.Skip("cli/ is not here")
	}
	t.Fatal("dead assertion")
}
`)
}

func TestASkipOneCallAwayFromTheArmIsASite(t *testing.T) {
	wantOneFailingSite(t, `
func bail(t *testing.T, msg string) {
	t.Skip(msg)
}

func TestOuter(t *testing.T) {
	if !cliPresent() {
		bail(t, "cli/ is not here")
	}
	t.Fatal("dead assertion")
}
`)
}

func TestAGuardFromAMethodIsASite(t *testing.T) {
	// The predicate is a method, so its receiver's type is unknown here. The
	// method name is unambiguous in the package, so the body is read.
	wantOneFailingSite(t, `
type probe struct{ dir string }

func (p probe) present() bool {
	info, err := os.Stat(cliSrc)
	return err == nil && info.IsDir()
}

func TestOuter(t *testing.T) {
	p := probe{dir: cliSrc}
	if !p.present() {
		t.Skip("cli/ is not here")
	}
	t.Fatal("dead assertion")
}
`)
}

func TestAMethodGuardOverAReceiverFieldIsUnresolved(t *testing.T) {
	// The path lives in the receiver's own field, and there is no value here to
	// read it from. UNRESOLVED and red -- never a quiet pass over what the gate
	// did not read.
	o := wantOneFailingSite(t, `
type probe struct{ dir string }

func (p probe) present() bool {
	info, err := os.Stat(p.dir)
	return err == nil && info.IsDir()
}

func TestOuter(t *testing.T) {
	p := probe{dir: cliSrc}
	if !p.present() {
		t.Skip("cli/ is not here")
	}
	t.Fatal("dead assertion")
}
`)
	if o.Sites[0].Verdict != Unresolved {
		t.Fatalf("a path in the receiver's field is not readable here; want Unresolved, got %v",
			o.Sites[0].Verdict)
	}
}

func TestATwoWayAmbiguousMethodGuardIsUnresolved(t *testing.T) {
	// Two types declare `present()`. WHICH body runs is the one question no
	// parse-only resolver can answer, so it answers UNRESOLVED instead of
	// picking one and reporting a verdict with a straight face.
	o := wantOneFailingSite(t, `
type a struct{}
type b struct{}

func (x a) present() bool {
	info, err := os.Stat(cliSrc)
	return err == nil && info.IsDir()
}

func (x b) present() bool {
	return true
}

func TestOuter(t *testing.T) {
	var x a
	if !x.present() {
		t.Skip("cli/ is not here")
	}
	t.Fatal("dead assertion")
}
`)
	if o.Sites[0].Verdict != Unresolved {
		t.Fatalf("an ambiguous method name must be unresolved, got %v", o.Sites[0].Verdict)
	}
}

func TestASkipCarriedByAMethodHelperIsASite(t *testing.T) {
	wantOneFailingSite(t, `
type kit struct{ t *testing.T }

func (k kit) skipUnless(ok bool, msg string) {
	if !ok {
		k.t.Skip(msg)
	}
}

func TestOuter(t *testing.T) {
	k := kit{t: t}
	k.skipUnless(cliPresent(), "cli/ is not here")
	t.Fatal("dead assertion")
}
`)
}

// ---------------------------------------------------------------------------
// what the widened reading must NOT do
// ---------------------------------------------------------------------------

func TestAHelperThatSkipsOnPRESENCEIsNotASite(t *testing.T) {
	// Polarity survives the call boundary: the helper is READ, not guessed at
	// from its name. A skip that fires when the path IS there cannot be caused
	// by a missing path, which is the only thing this gate is about.
	wantNoSite(t, `
func skipIf(t *testing.T, cond bool, msg string) {
	if cond {
		t.Skip(msg)
	}
}

func TestOuter(t *testing.T) {
	skipIf(t, cliPresent(), "cli/ is HERE, and this test wants it gone")
	t.Log("runs when the path is absent")
}
`)
}

func TestATaggedSwitchIsNotReadAsAGuard(t *testing.T) {
	// `switch x { case ok: }` compares values. A predicate's answer appearing
	// there is not the predicate's own polarity, and reading it would be a
	// guess -- the failure mode B-183 was opened for, in the other direction.
	wantNoSite(t, `
func TestOuter(t *testing.T) {
	ok := cliPresent()
	switch ok {
	case false:
		t.Skip("cli/ is not here")
	}
	t.Fatal("assertion")
}
`)
}

func TestAnImportedPackageIsNotReadAsAMethodOfThisOne(t *testing.T) {
	// `probe.Present()` names an IMPORT here. A package that happens to declare
	// a method called Present must not lend its body to it: reading a body the
	// code does not call is a guess wearing a verdict's clothes.
	src := `package unit

import (
	"os"
	"testing"

	"example.com/probe"
)

const cliSrc = "../../../cli/src"

type local struct{}

func (l local) Present() bool {
	info, err := os.Stat(cliSrc)
	return err == nil && info.IsDir()
}

func TestOuter(t *testing.T) {
	if !probe.Present() {
		t.Skip("something this gate has no opinion about")
	}
	t.Fatal("assertion")
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})
	if o := mustRun(t, root); len(o.Sites) != 0 {
		t.Fatalf("an import's function is not this package's method; got %d site(s): %+v",
			len(o.Sites), o.Sites)
	}
}

func TestACyclicSkipHelperTerminates(t *testing.T) {
	// Two helpers that hand the skip to each other. The visited set has to end
	// the walk: a gate that hangs on a cyclic helper is a gate nobody runs.
	o := mustRun(t, treeWith(t, `
func bounceA(t *testing.T, ok bool) { bounceB(t, ok) }

func bounceB(t *testing.T, ok bool) {
	if !ok {
		t.Skip("cli/ is not here")
	}
	bounceA(t, ok)
}

func TestOuter(t *testing.T) {
	bounceA(t, cliPresent())
	t.Fatal("dead assertion")
}
`))
	if len(o.Sites) == 0 {
		t.Fatalf("the skip is two hops away and still reachable; want a site, got none")
	}
	if o.Pass {
		t.Fatalf("cli/src is absent and undeclared; violations:\n%s", joined(o.Violations))
	}
}

// ---------------------------------------------------------------------------
// the limits this package declares, held to their word
// ---------------------------------------------------------------------------

func TestAGuardOnAStructFieldIsStillNotASite(t *testing.T) {
	// Declared in SCOPE. The guard is a FIELD, not a call, so there is no body
	// to read. If this ever becomes a site, the SCOPE paragraph is what has to
	// change with it -- a SCOPE that describes a limit somebody fixed lies in
	// the direction nobody checks.
	wantNoSite(t, `
type flags struct{ havePath bool }

func TestOuter(t *testing.T) {
	f := flags{havePath: cliPresent()}
	if !f.havePath {
		t.Skip("cli/ is not here")
	}
	t.Fatal("dead assertion")
}
`)
}

func TestASkipThroughAFunctionValueIsStillInvisible(t *testing.T) {
	// Declared in SCOPE, and it is the sharper of the two: `skip := t.Skip`
	// makes the call a bare identifier, so the skip is neither a site NOR
	// counted by the floor.
	//
	// The second test is here to hold the counting floor up: without a skip the
	// counter CAN see, Check reports a broken setup rather than a verdict, and
	// this test would be asserting about an error instead of about the limit.
	o := wantNoSite(t, `
func TestOuter(t *testing.T) {
	skip := t.Skip
	if !cliPresent() {
		skip("cli/ is not here")
	}
	t.Fatal("dead assertion")
}

func TestCounted(t *testing.T) {
	t.Skip("bare, and the only skip this gate can count in this file")
}
`)
	if o.SkipCalls != 1 {
		t.Fatalf("only the bare skip is countable here; got %d -- if that changed, SCOPE must change with it",
			o.SkipCalls)
	}
}

func TestAHelperWithItsOwnGuardIsReportedOnceWhereItIsWritten(t *testing.T) {
	// Following a skip into a helper made one guard into one finding PER CALLER.
	// Measured 2026-09-05 in infra/tests/config/: one guarded helper with two
	// callers produced three violations naming three different functions, so the
	// pin file would grow a line every time somebody called it again. The guard
	// belongs to the line that writes it; only a guard the CALLER supplies is
	// reported at the call.
	o := mustRun(t, treeWith(t, `
func skipIfNoCLI(t *testing.T) {
	if _, err := os.Stat(cliSrc); err != nil {
		t.Skip("cli/ is not here")
	}
}

func TestOne(t *testing.T) {
	skipIfNoCLI(t)
	t.Fatal("dead assertion")
}

func TestTwo(t *testing.T) {
	skipIfNoCLI(t)
	t.Fatal("dead assertion")
}
`))
	if len(o.Sites) != 1 {
		t.Fatalf("one guard, one finding; got %d: %+v", len(o.Sites), o.Sites)
	}
	if o.Sites[0].Func != "skipIfNoCLI" {
		t.Fatalf("the finding belongs to the helper that writes the guard; got %q", o.Sites[0].Func)
	}
}

func TestAGuardInAHelperOutsideTheTestFilesIsReportedAtTheCall(t *testing.T) {
	// The other side of the same rule. A helper in a non-test file of the same
	// package is never walked as a function in its own right -- scanDir analyses
	// only _test.go -- so suppressing it at the call would be a silence, not a
	// deduplication.
	root := tree(t, map[string]string{
		"infra/quality/pin.txt": pinHeader,
		"infra/tests/unit/helpers.go": `package unit

import (
	"os"
	"testing"
)

const cliSrc = "../../../cli/src"

func skipIfNoCLI(t *testing.T) {
	if _, err := os.Stat(cliSrc); err != nil {
		t.Skip("cli/ is not here")
	}
}
`,
		// The bare skip holds the counting floor up: the scan counts skips in
		// _test.go files only, and the helper's own skip is not in one.
		"infra/tests/unit/a_test.go": `package unit

import "testing"

func TestOuter(t *testing.T) {
	skipIfNoCLI(t)
	t.Fatal("dead assertion")
}

func TestCounted(t *testing.T) {
	t.Skip("bare")
}
`,
	})
	o := mustRun(t, root)
	if len(o.Sites) != 1 {
		t.Fatalf("nothing else reports this guard, so the call site must; got %d: %+v",
			len(o.Sites), o.Sites)
	}
	if o.Sites[0].Func != "TestOuter" || o.Pass {
		t.Fatalf("want the call site red; got func=%q pass=%v", o.Sites[0].Func, o.Pass)
	}
}

func TestASkipCarriedByAnotherPackagesHelperIsStillInvisible(t *testing.T) {
	// Declared in SCOPE. This gate binds one directory at a time, so the
	// helper's body was never parsed and there is nothing to read the polarity
	// from. The counted skip keeps the floor up so this measures the limit
	// rather than a broken setup.
	src := `package unit

import (
	"os"
	"testing"

	"example.com/helpers"
)

const cliSrc = "../../../cli/src"

func cliPresent() bool {
	info, err := os.Stat(cliSrc)
	return err == nil && info.IsDir()
}

func TestOuter(t *testing.T) {
	helpers.SkipUnless(t, cliPresent(), "cli/ is not here")
	t.Fatal("dead assertion")
}

func TestCounted(t *testing.T) {
	t.Skip("bare")
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})
	if o := mustRun(t, root); len(o.Sites) != 0 {
		t.Fatalf("a body this gate never parsed cannot be a site; got %d: %+v", len(o.Sites), o.Sites)
	}
}

func TestFollowingASkipThroughHelpersIsBoundedAndTheBoundIsWhereItSays(t *testing.T) {
	// The bound is a declared SILENCE, unlike every other bound in this gate,
	// so it has to be measured at both sides of the edge or the SCOPE paragraph
	// is a guess. A chain of exactly maxDelegatedSkipDepth frames is read; one
	// more is not.
	chain := func(hops int) string {
		src := "\nfunc TestOuter(t *testing.T) {\n\thop1(t, cliPresent())\n\tt.Fatal(\"dead assertion\")\n}\n"
		for i := 1; i <= hops; i++ {
			body := fmt.Sprintf("\thop%d(t, ok)\n", i+1)
			if i == hops {
				body = "\tif !ok {\n\t\tt.Skip(\"cli/ is not here\")\n\t}\n"
			}
			src += fmt.Sprintf("\nfunc hop%d(t *testing.T, ok bool) {\n%s}\n", i, body)
		}
		return src
	}
	if o := mustRun(t, treeWith(t, chain(maxDelegatedSkipDepth))); len(o.Sites) != 1 {
		t.Fatalf("a chain of %d frames is inside the bound and must be read; got %d site(s)",
			maxDelegatedSkipDepth, len(o.Sites))
	}
	if o := mustRun(t, treeWith(t, chain(maxDelegatedSkipDepth+1))); len(o.Sites) != 0 {
		t.Fatalf("past the bound the walk is silent, which is what SCOPE declares; got %d site(s): %+v",
			len(o.Sites), o.Sites)
	}
}
