// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package testskipabsentpath

import (
	"strings"
	"testing"
)

// B-193, seventh round. A local the guard reads is decidable only where every
// statement between its declaration and the guard certainly runs.
//
// Measured 2026-09-06 with a binary built from this package, injected into
// infra/tests/config/: four spellings SILENT at exit 0 --
//
//	f := cliPresent; defer func() { f = dialCluster }(); if !f() { t.Skip() }
//	the same through t.Cleanup
//	f := cliPresent; if os.Getenv("X") != "" { f = dialCluster }; if !f() { ... }
//	for _, f := range probes { if !f() { t.Skip() } }
//
// -- and one WORSE than silent:
//
//	f := cliPresent; if cond { f = otherPresent }; if !f() { ... }
//
// which reported ABSENT naming otherPresent's path. The walk is textual, so the
// last `=` it passed won, with no awareness that a branch may not run. The
// package doc refuses exactly that for package-level names -- "a confident
// verdict about a function that never ran" -- and the resolver's own locals map
// did it.
//
// A confident wrong verdict is worse than a silence, so where branch awareness
// is out of reach the answer is UNRESOLVED. It is not out of reach where every
// statement runs: TestALocalReassignedBeforeTheGuardReadsTheSecondValue pins
// the straight-line case and refused a coarser version of this rule.

// twoProbeTree renders a package with TWO filesystem predicates over two
// different paths, so a verdict that names one of them can be caught naming the
// wrong one.
func twoProbeTree(t *testing.T, decls, guard string) string {
	t.Helper()
	return funcValueTree(t, `
const otherDir = "../../../other/src"

func otherPresent() bool {
	info, err := os.Stat(otherDir)
	return err == nil && info.IsDir()
}

func alwaysTrue() bool { return true }
`+decls, guard)
}

// wantUnresolvedNamingNeither asserts the gate said it could not read the guard
// AND did not name either candidate path while saying so. The second half is
// the point of the round: an UNRESOLVED that still prints one of the two paths
// as the answer is the same overreach in quieter clothes.
func wantUnresolvedNamingNeither(t *testing.T, root string) {
	t.Helper()
	o := mustRun(t, root)
	if len(o.Sites) != 1 {
		t.Fatalf("want exactly one site; got %d: %+v", len(o.Sites), o.Sites)
	}
	if o.Sites[0].Verdict != Unresolved {
		t.Fatalf("want UNRESOLVED, got verdict %v for %q", o.Sites[0].Verdict, o.Sites[0].Expr)
	}
	if o.Pass {
		t.Fatal("a guard this gate could not read must fail, not pass")
	}
	got := joined(o.Violations)
	for _, path := range []string{"cli/src", "other/src"} {
		if strings.Contains(got, path) {
			t.Fatalf("the verdict must not name %q as the guarded path when it could not decide which value the guard reads; got:\n%s",
				path, got)
		}
	}
}

// ---------------------------------------------------------------------------
// the assignment the guard may not have seen
// ---------------------------------------------------------------------------

func TestALocalReboundInsideADeferredClosureIsUnresolvedRatherThanSilent(t *testing.T) {
	// The deferred assignment runs AFTER the guard, so the walk reading it as
	// the live value is not merely undecidable, it is backwards. It was silent:
	// the closure's value touches no filesystem, so the name stopped clearing
	// the bar the guard had already cleared one line above.
	wantUnresolvedNamingNeither(t, twoProbeTree(t, "",
		"\tf := cliPresent\n\tdefer func() { f = alwaysTrue }()\n\tif !f() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestALocalReboundInsideACleanupClosureIsUnresolvedRatherThanSilent(t *testing.T) {
	wantUnresolvedNamingNeither(t, twoProbeTree(t, "",
		"\tf := cliPresent\n\tt.Cleanup(func() { f = alwaysTrue })\n\tif !f() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestALocalReboundInsideAGoClosureIsUnresolvedRatherThanSilent(t *testing.T) {
	wantUnresolvedNamingNeither(t, twoProbeTree(t, "",
		"\tf := cliPresent\n\tgo func() { f = alwaysTrue }()\n\tif !f() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestALocalReboundInABranchToANonProbeIsUnresolvedRatherThanSilent(t *testing.T) {
	// The branch may not run, so the guard may still be reading cliPresent.
	// Reading the branch's value made the name stop clearing the bar entirely,
	// which is how a live guard bought silence for one line of refactoring.
	wantUnresolvedNamingNeither(t, twoProbeTree(t, "",
		"\tf := cliPresent\n\tif os.Getenv(\"X\") != \"\" {\n\t\tf = alwaysTrue\n\t}\n\tif !f() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestALocalReboundInABranchToAnotherProbeNamesNeitherPath(t *testing.T) {
	// The sharpest one, because it was WORSE than silence: this reported ABSENT
	// naming other/src, a path chosen only by being the last `=` the walk
	// passed. Either path could be the one the guard reads, so the gate says it
	// cannot tell rather than picking.
	wantUnresolvedNamingNeither(t, twoProbeTree(t, "",
		"\tf := cliPresent\n\tif os.Getenv(\"X\") != \"\" {\n\t\tf = otherPresent\n\t}\n\tif !f() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestALocalReboundInASwitchCaseIsUnresolvedRatherThanSilent(t *testing.T) {
	wantUnresolvedNamingNeither(t, twoProbeTree(t, "",
		"\tf := cliPresent\n\tswitch {\n\tcase os.Getenv(\"X\") != \"\":\n\t\tf = otherPresent\n\t}\n\tif !f() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestALocalReboundInsideAClosureHandedToAHelperIsUnresolved(t *testing.T) {
	// The helper decides whether the closure ever runs, and this gate does not
	// read that decision.
	wantUnresolvedNamingNeither(t, twoProbeTree(t, `
func runNow(fn func()) { fn() }
`,
		"\tf := cliPresent\n\trunNow(func() { f = alwaysTrue })\n\tif !f() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

// ---------------------------------------------------------------------------
// the guard that comes out of a range
// ---------------------------------------------------------------------------

func TestARangeValueOverATableOfPredicatesIsUnresolvedRatherThanSilent(t *testing.T) {
	// `probes[k]()` with an unreadable key is UNRESOLVED and red. Ranging the
	// SAME map was silent -- one spelling of one question answered two ways,
	// which is the gap this item keeps reopening on.
	wantUnresolvedSite(t, funcValueTree(t, `
var probes = map[string]func() bool{"cli": cliPresent}`,
		"\tfor _, f := range probes {\n\t\tif !f() {\n\t\t\tt.Skip(\"cli/ not present\")\n\t\t}\n\t}"),
		"table of predicates")
}

func TestARangeValueOverASliceOfPredicatesIsUnresolvedRatherThanSilent(t *testing.T) {
	wantUnresolvedSite(t, funcValueTree(t, `
var probes = []func() bool{cliPresent}`,
		"\tfor _, f := range probes {\n\t\tif !f() {\n\t\t\tt.Skip(\"cli/ not present\")\n\t\t}\n\t}"),
		"table of predicates")
}

// ---------------------------------------------------------------------------
// the same doubt on a PATH local, which is the door the func-value fix left open
// ---------------------------------------------------------------------------

func TestAPathLocalReboundInABranchNamesNeitherPath(t *testing.T) {
	// Measured reopening the item after the func-value half was fixed: this
	// reported ABSENT naming other/src. Same walk, same last-write-wins, one
	// door over.
	wantUnresolvedNamingNeither(t, twoProbeTree(t, "",
		"\tp := cliDir\n\tif os.Getenv(\"X\") != \"\" {\n\t\tp = otherDir\n\t}\n\tif _, err := os.Stat(p); os.IsNotExist(err) {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestAPathLocalReboundInsideADeferredClosureNamesNeitherPath(t *testing.T) {
	// And this one was not even a guess: the deferred assignment has provably
	// not run when os.Stat reads p, so the reported other/src was the one path
	// it could not be.
	wantUnresolvedNamingNeither(t, twoProbeTree(t, "",
		"\tp := cliDir\n\tdefer func() { p = otherDir }()\n\tif _, err := os.Stat(p); os.IsNotExist(err) {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

// ---------------------------------------------------------------------------
// the bar does not drop, and neither does what was already decidable
// ---------------------------------------------------------------------------

func TestAGuardReadBeforeTheRebindingKeepsItsVerdict(t *testing.T) {
	// The assignment is AFTER the guard, so it cannot have changed what the
	// guard read. Making the rebound case unresolved must not cost this one.
	wantAbsentSite(t, twoProbeTree(t, "",
		"\tf := cliPresent\n\tif !f() {\n\t\tt.Skip(\"cli/ not present\")\n\t}\n\tf = otherPresent\n\t_ = f"))
}

func TestAnIfInitAssignmentRunsUnconditionallyAndKeepsItsVerdict(t *testing.T) {
	// An if's INIT statement always runs, so the name it assigns is decided
	// before the condition reads it.
	wantAbsentSite(t, twoProbeTree(t, "",
		"\tf := alwaysTrue\n\tif f = cliPresent; !f() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestALocalReboundBetweenTwoNonFilesystemPredicatesIsNotASite(t *testing.T) {
	// The control the whole round is measured against: the doubt only costs a
	// verdict where something bound under the name touches the filesystem.
	wantSilentPass(t, notAProbeTree(t, `
func alsoDial() bool { return false }`,
		"\tf := dialCluster\n\tif len(t.Name()) > 0 {\n\t\tf = alsoDial\n\t}\n\tif !f() {\n\t\tt.Skip(\"cluster unreachable\")\n\t}"),
		"a rebound local holding cluster predicates clears no bar")
}

func TestARangeOverANonFilesystemTableIsNotASite(t *testing.T) {
	wantSilentPass(t, notAProbeTree(t, `
var dials = []func() bool{dialCluster}`,
		"\tfor _, f := range dials {\n\t\tif !f() {\n\t\t\tt.Skip(\"cluster unreachable\")\n\t\t}\n\t}"),
		"a table of cluster predicates clears no bar, ranged or indexed")
}
