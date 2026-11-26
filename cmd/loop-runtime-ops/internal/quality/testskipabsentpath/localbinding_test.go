// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package testskipabsentpath

import "testing"

// B-193. A guard reached through a LOCAL name, a pair of parentheses, or a
// field declared by a named func type.
//
// The sixth consecutive reopening of one class, and the cheapest yet: one line
// of ordinary refactoring -- `f := <the spelling that is already red>` --
// restored total silence over every UNRESOLVED case, because the fallback whose
// whole job is to turn a silence into a verdict was reachable only through the
// PACKAGE-LEVEL indexes and a local name is in none of them. Measured on
// 2026-09-05 by an independent audit with the B-189 binary, inside
// infra/tests/config/: eight spellings, gate PASS, a dead t.Fatal under each.

func TestALocalHoldingAReturnedFuncValueIsRead(t *testing.T) {
	// `presentFn()()` is ABSENT and red. The same value through one local was
	// silent -- and the value is just as followable with the binding as without.
	wantAbsentSite(t, funcValueTree(t, `
func presentFn() func() bool { return cliPresent }`,
		"\tf := presentFn()\n\tif !f() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestALocalHoldingAFieldFromElsewhereIsUnresolvedRatherThanSilent(t *testing.T) {
	// `s.probe()` is UNRESOLVED. `p := s.probe; p()` is the same unreadable
	// field, and the verdict has to survive the binding.
	wantUnresolvedSite(t, funcValueTree(t, `
type holder struct{ probe func() bool }

func newHolder() holder { return holder{probe: cliPresent} }`,
		"\ts := newHolder()\n\tp := s.probe\n\tif !p() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"),
		"func-valued field")
}

func TestALocalHoldingATableEntryIsUnresolvedRatherThanSilent(t *testing.T) {
	// `probes[k]()` is UNRESOLVED. `f := probes[k]; f()` was not.
	wantUnresolvedSite(t, funcValueTree(t, `
var probes = map[string]func() bool{"cli": cliPresent}`,
		"\tk := \"c\" + \"li\"\n\tf := probes[k]\n\tif !f() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"),
		"table of predicates")
}

func TestALocalHoldingAValueReturnedByTwoReturnsIsUnresolvedRatherThanSilent(t *testing.T) {
	// The producer's declared limit has to survive the binding too: a second
	// return is a guess about which one ran, said out loud rather than passed.
	wantUnresolvedSite(t, funcValueTree(t, `
func presentFn(fast bool) func() bool {
	if fast {
		return func() bool { return true }
	}
	return cliPresent
}`,
		"\tf := presentFn(false)\n\tif !f() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"),
		"value returned by a call")
}

func TestALocalHoldingAnAmbiguousMethodValueIsUnresolvedRatherThanSilent(t *testing.T) {
	// Two types declaring one method name, one of them touching the filesystem.
	// Reading either body is a guess about a receiver this gate cannot type.
	wantUnresolvedSite(t, funcValueTree(t, `
type a struct{}

type b struct{}

func (a) Present() bool { return cliPresent() }

func (b) Present() bool { return true }`,
		"\tx := a{}\n\tp := x.Present\n\tif !p() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"),
		"Present()")
}

// ---------------------------------------------------------------------------
// the bar does not drop: a local carrying a predicate that touches no file
// stays none of this gate's business
// ---------------------------------------------------------------------------

// notAProbeTree renders a package whose predicates touch nothing on disk.
func notAProbeTree(t *testing.T, decls, guard string) string {
	t.Helper()
	src := `package unit

import "testing"

func dialCluster() bool { return true }
` + decls + `

func TestGuarded(t *testing.T) {
` + guard + `
	t.Log("assertion")
}
`
	return tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})
}

func wantSilentPass(t *testing.T, root, why string) {
	t.Helper()
	o := mustRun(t, root)
	if len(o.Sites) != 0 || !o.Pass {
		t.Fatalf("%s; got %d site(s), pass=%v:\n%s", why, len(o.Sites), o.Pass, joined(o.Violations))
	}
}

func TestALocalHoldingANonFilesystemPredicateIsNotASite(t *testing.T) {
	// `dialFn()` is the control the whole item is measured against: making a
	// local readable must not make every local a verdict.
	wantSilentPass(t, notAProbeTree(t, "",
		"\tdialFn := dialCluster\n\tif !dialFn() {\n\t\tt.Skip(\"cluster unreachable\")\n\t}"),
		"a local holding a predicate that touches no filesystem is not this gate's business")
}

func TestALocalHoldingAnUnreadableNonFilesystemTableEntryIsNotASite(t *testing.T) {
	wantSilentPass(t, notAProbeTree(t, `
var dials = map[string]func() bool{"prod": dialCluster}`,
		"\tk := \"pr\" + \"od\"\n\tf := dials[k]\n\tif !f() {\n\t\tt.Skip(\"cluster unreachable\")\n\t}"),
		"a table of cluster predicates clears no bar, bound to a local or not")
}

func TestALocalHoldingAnUnreadableNonFilesystemFieldIsNotASite(t *testing.T) {
	wantSilentPass(t, notAProbeTree(t, `
type holder struct{ ready func() bool }

func newHolder() holder { return holder{ready: dialCluster} }`,
		"\ts := newHolder()\n\tp := s.ready\n\tif !p() {\n\t\tt.Skip(\"cluster unreachable\")\n\t}"),
		"a field nothing fills with a probe clears no bar, bound to a local or not")
}

// ---------------------------------------------------------------------------
// the two smaller ones: a field declared by a NAMED func type, and one pair of
// parentheses
// ---------------------------------------------------------------------------

func TestAFieldDeclaredThroughANamedFuncTypeIsIndexed(t *testing.T) {
	// `probe predicate` is the same field as `probe func() bool`. Matching only
	// the literal func type left it out of the index, so the declared bar was
	// MET and the guard walked past anyway -- a documented property the code did
	// not have.
	wantUnresolvedSite(t, funcValueTree(t, `
type predicate func() bool

type holder struct{ probe predicate }

func newHolder() holder { return holder{probe: cliPresent} }`,
		"\ts := newHolder()\n\tif !s.probe() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"),
		"func-valued field")
}

func TestAFieldDeclaredThroughANamedFuncTypeStillNeedsAProbe(t *testing.T) {
	wantSilentPass(t, notAProbeTree(t, `
type predicate func() bool

type holder struct{ ready predicate }

func newHolder() holder { return holder{ready: dialCluster} }`,
		"\ts := newHolder()\n\tif !s.ready() {\n\t\tt.Skip(\"cluster unreachable\")\n\t}"),
		"indexing the named-type field must not turn every one of them into a verdict")
}

func TestAParenthesisedPredicateIsRead(t *testing.T) {
	// Stable under gofmt, and routed to no reader at all before B-193.
	wantAbsentSite(t, funcValueTree(t, "",
		"\tif !(cliPresent)() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestAFuncLiteralInvokedOnTheSpotIsRead(t *testing.T) {
	wantAbsentSite(t, funcValueTree(t, "",
		"\tif !func() bool {\n\t\tinfo, err := os.Stat(cliDir)\n\t\treturn err == nil && info.IsDir()\n\t}() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

// ---------------------------------------------------------------------------
// `rebound` stops accusing a rebinding that did not happen
// ---------------------------------------------------------------------------

func TestAnAliasShadowedByALocalIsNotReportedAsRebound(t *testing.T) {
	// The alias is never reassigned. An unrelated function declaring its own
	// `present` used to downgrade it to UNRESOLVED under a printed sentence --
	// "name rebound to another func value" -- asserting a swap that never
	// happened, which sends the reader looking for an init() that is not there.
	wantAbsentSite(t, funcValueTree(t, `
func alwaysTrue() bool { return true }

func alwaysFalse() bool { return false }

var present = cliPresent

func unrelated() bool {
	present := alwaysTrue
	present = alwaysFalse
	return present()
}`,
		"\tif !present() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestAnAliasShadowedInsideAClosureIsNotReportedAsRebound(t *testing.T) {
	// A closure assigning to a captured local rebinds the local.
	wantAbsentSite(t, funcValueTree(t, `
func alwaysTrue() bool { return true }

var present = cliPresent

func unrelated() func() {
	present := alwaysTrue
	return func() { present = alwaysTrue }
}`,
		"\tif !present() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestARealPackageLevelRebindingIsStillUnresolved(t *testing.T) {
	// The scope tracking must not become a blanket amnesty: the local's block
	// has CLOSED by the time the package-level name is assigned, and that
	// assignment is exactly the swap B-189 refused to read past.
	wantUnresolvedSite(t, funcValueTree(t, `
func alwaysTrue() bool { return true }

var present = alwaysTrue

func swap() {
	{
		present := cliPresent
		_ = present
	}
	present = cliPresent
}`,
		"\tif !present() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"),
		"rebound to another func value")
}

func TestAnUnfollowableNameIsNotAccusedOfBeingRebound(t *testing.T) {
	// A package name whose value this gate cannot follow was refused under the
	// rebinding sentence too. It is a different measurement and now says so.
	wantUnresolvedSite(t, funcValueTree(t, `
var probes = map[string]func() bool{"cli": cliPresent}

var key = "cli"

var present = probes[key]`,
		"\tif !present() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"),
		"table of predicates")
}

func TestANameThisGateCannotFollowSaysThatAndNotThatItWasRebound(t *testing.T) {
	// The pooling `pkgScope` documents, seen from the reason line: the NAME
	// clears the bar because a composite literal elsewhere fills a field spelled
	// the same, while the value the guard actually names is a body in another
	// package. UNRESOLVED is right; "rebound" would not be, and used to be what
	// it said.
	wantUnresolvedSite(t, funcValueTree(t, `
type holder struct{ present func() bool }

var seed = holder{present: cliPresent}

var present = elsewhere.Present`,
		"\tif !present() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"),
		"could not follow")
}

func TestALocalShadowingAPackageFuncIsNotReadAsThatFunc(t *testing.T) {
	// The SELECTOR names the method, not the call's own Fun. Looking `p` up
	// among the package's functions reads a body the code never enters -- and
	// here that body clears no bar, so the guard would go quiet.
	wantUnresolvedSite(t, funcValueTree(t, `
type holder struct{ probe func() bool }

func newHolder() holder { return holder{probe: cliPresent} }

func p() bool { return true }`,
		"\ts := newHolder()\n\tp := s.probe\n\tif !p() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"),
		"func-valued field")
}

// ---------------------------------------------------------------------------
// the reopening attempts
// ---------------------------------------------------------------------------

func TestALocalGuardInsideADeferIsRead(t *testing.T) {
	wantAbsentSite(t, funcValueTree(t, `
func presentFn() func() bool { return cliPresent }`,
		"\tdefer func() {\n\t\tf := presentFn()\n\t\tif !f() {\n\t\t\tt.Skip(\"cli/ not present\")\n\t\t}\n\t}()"))
}

func TestALocalHoldingAPointerReceiverMethodValueIsRead(t *testing.T) {
	wantAbsentSite(t, funcValueTree(t, `
type holder struct{}

func (h *holder) Present() bool { return cliPresent() }`,
		"\th := &holder{}\n\tp := h.Present\n\tif !p() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestALocalHoldingATableEntryWithAComputedKeyInAHelperIsReadAtTheCall(t *testing.T) {
	// The guard is the CALLER's func value, handed across a call and then bound
	// to a local inside the helper. Reported at the call, once.
	src := `package unit

import (
	"os"
	"testing"
)

func cliPresent() bool {
	info, err := os.Stat("../../../cli/src")
	return err == nil && info.IsDir()
}

func runWith(t *testing.T, present func() bool) {
	p := present
	if !p() {
		t.Skip("cli/ not present")
	}
	t.Fatal("dead assertion under a skip that always fires")
}

func TestGuarded(t *testing.T) {
	runWith(t, cliPresent)
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})
	o := mustRun(t, root)
	if len(o.Sites) != 1 {
		t.Fatalf("want exactly one site, reported once; got %d: %+v", len(o.Sites), o.Sites)
	}
	if o.Sites[0].Func != "TestGuarded" {
		t.Fatalf("a guard the caller supplied belongs to the caller's line; got %q", o.Sites[0].Func)
	}
	if o.Sites[0].Verdict != Absent || o.Pass {
		t.Fatalf("want a failing ABSENT verdict; got %v, pass=%v", o.Sites[0].Verdict, o.Pass)
	}
}

// The three below were found by the sixth round's own reopening attempt, run
// against the fix rather than against the bug: twelve spellings were injected
// into infra/tests/config/ and measured with binaries built from this package,
// nine of which the B-189 build already caught. These three it did not.

func TestAParenthesisedFuncLiteralInvokedOnTheSpotIsRead(t *testing.T) {
	wantAbsentSite(t, funcValueTree(t, "",
		"\tif !(func() bool {\n\t\t_, err := os.Stat(cliDir)\n\t\treturn err == nil\n\t})() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestAParenthesisedLocalIsRead(t *testing.T) {
	wantAbsentSite(t, funcValueTree(t, "",
		"\tf := cliPresent\n\tif !(f)() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestALocalHoldingAFieldInASwitchCaseIsUnresolvedRatherThanSilent(t *testing.T) {
	// A tag-less switch case is an if written with different punctuation, and
	// the local binding has to survive that spelling too.
	wantUnresolvedSite(t, funcValueTree(t, `
type holder struct{ probe func() bool }

func newHolder() holder { return holder{probe: cliPresent} }`,
		"\ts := newHolder()\n\tp := s.probe\n\tswitch {\n\tcase !p():\n\t\tt.Skip(\"cli/ not present\")\n\t}"),
		"func-valued field")
}

func TestACrossFrameFuncValueCalledWithArgumentsStaysADeclaredLimit(t *testing.T) {
	// The caller's value is read in the CALLER's scope; the arguments at the
	// call were written in the HELPER's. Reading `dir` over there answers about
	// a name that frame never bound -- and where the caller happens to bind the
	// same name to a different path, that is a confident verdict about the
	// wrong file, which is worse than the silence this shape already had. So a
	// cross-frame value invoked WITH arguments is left exactly where B-189 left
	// it, and this test says so rather than letting the limit be discovered.
	//
	// The zero-argument spelling -- the one a `func() bool` predicate actually
	// uses -- is read, and TestALocalHoldingATableEntryWithAComputedKeyInAHelperIsReadAtTheCall
	// holds it.
	src := `package unit

import (
	"os"
	"testing"
)

func cliPresentAt(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

func presentFnAt() func(string) bool { return cliPresentAt }

func runWith(t *testing.T, present func(string) bool) {
	dir := "../../../cli/src"
	if !present(dir) {
		t.Skip("cli/ not present")
	}
	t.Fatal("dead assertion under a skip that always fires")
}

func TestGuarded(t *testing.T) {
	runWith(t, presentFnAt())
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})
	wantSilentPass(t, root,
		"a cross-frame func value called with arguments is a declared limit, not a verdict")
}

func TestALocalReassignedBeforeTheGuardReadsTheSecondValue(t *testing.T) {
	// Locals are read in statement order, so the value the guard actually calls
	// is the one it gets -- not the one the name was born with.
	wantAbsentSite(t, funcValueTree(t, `
func alwaysTrue() bool { return true }`,
		"\tf := alwaysTrue\n\tf = cliPresent\n\tif !f() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}
