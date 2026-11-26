// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package testskipabsentpath

import (
	"strings"
	"testing"
)

// B-189. A guard whose predicate is reached through a name bound to a FUNCTION
// VALUE, rather than through a name declared as a function.
//
// pkgScope.callee resolved an identifier only against FuncDecls, so every
// spelling below was not found, not read and not reported. Not UNRESOLVED --
// SILENT, which is the one result this gate promises never to give. Measured on
// 2026-09-05 with a binary built from this package, the six injected into
// infra/tests/config/: 708 files, 144 skip calls, 11 path-guarded, PASS, exit 0,
// with a dead t.Fatal under each and every one of the six really skipping.
//
// Each is one line away from the live `if !cliSrcPresent() { t.Skip() }` that
// opened B-183, B-186 and B-188 -- the fourth consecutive reopening of one
// class, and the first that went quiet instead of red.

// funcValueTree renders a package whose guard is spelled by `guard`, over
// declarations `decls`, with a dead assertion under the skip.
func funcValueTree(t *testing.T, decls, guard string) string {
	t.Helper()
	src := `package unit

import (
	"os"
	"testing"
)

const cliDir = "../../../cli/src"

func cliPresent() bool {
	info, err := os.Stat(cliDir)
	return err == nil && info.IsDir()
}
` + decls + `

func TestGuarded(t *testing.T) {
` + guard + `
	t.Fatal("dead assertion under a skip that always fires")
}
`
	return tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})
}

// wantAbsentSite asserts the guard was read all the way to a path this tree does
// not have -- a real verdict, not merely a refusal to stay quiet.
func wantAbsentSite(t *testing.T, root string) {
	t.Helper()
	o := mustRun(t, root)
	if len(o.Sites) != 1 {
		t.Fatalf("want exactly one site; got %d: %+v", len(o.Sites), o.Sites)
	}
	if o.Sites[0].Verdict != Absent {
		t.Fatalf("want the guard read to an ABSENT path, got verdict %v for %q",
			o.Sites[0].Verdict, o.Sites[0].Expr)
	}
	if o.Pass {
		t.Fatal("an absent, undeclared path-guarded skip must fail the gate")
	}
}

// wantUnresolvedSite asserts the guard could not be read AND that the gate said
// so instead of walking past it.
func wantUnresolvedSite(t *testing.T, root, reason string) {
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
	if !strings.Contains(joined(o.Violations), reason) {
		t.Fatalf("the violation must say WHY it could not be read (%q); got:\n%s",
			reason, joined(o.Violations))
	}
}

func TestAPackageAliasOfAPredicateIsRead(t *testing.T) {
	wantAbsentSite(t, funcValueTree(t,
		"\nvar cliSrcPresentAlias = cliPresent",
		"\tif !cliSrcPresentAlias() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestAPackageVarHoldingAFuncLiteralIsRead(t *testing.T) {
	wantAbsentSite(t, funcValueTree(t, `
var present = func() bool {
	info, err := os.Stat(cliDir)
	return err == nil && info.IsDir()
}`,
		"\tif !present() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestALocalBoundToAPredicateIsRead(t *testing.T) {
	wantAbsentSite(t, funcValueTree(t, "",
		"\tcheck := cliPresent\n\tif !check() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestAMethodValueIsRead(t *testing.T) {
	wantAbsentSite(t, funcValueTree(t, `
type holder struct{}

func (h holder) Present() bool { return cliPresent() }`,
		"\th := holder{}\n\tp := h.Present\n\tif !p() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestAStructFieldTheCallerFilledIsRead(t *testing.T) {
	// The one field read that is not a guess: the composite literal in the
	// caller's own scope says which func value the field holds.
	wantAbsentSite(t, funcValueTree(t, `
type holder struct{ probe func() bool }`,
		"\ts := holder{probe: cliPresent}\n\tif !s.probe() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestAStructFieldFromElsewhereIsUnresolvedRatherThanSilent(t *testing.T) {
	// Which value a field holds is dataflow, and this gate has none. Saying so
	// is the answer; picking one of the package's assignments would be the guess.
	wantUnresolvedSite(t, funcValueTree(t, `
type holder struct{ probe func() bool }

func newHolder() holder { return holder{probe: cliPresent} }`,
		"\ts := newHolder()\n\tif !s.probe() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"),
		"func-valued field")
}

func TestAFieldNothingFillsWithAProbeIsNotThisGatesBusiness(t *testing.T) {
	// The bar an unreadable guard has to clear before it costs a verdict, and it
	// is the same bar `if !dialCluster() { t.Skip() }` fails: nothing this
	// package puts in that field touches the filesystem.
	src := `package unit

import "testing"

type holder struct{ ready func() bool }

func reachable() bool { return true }

func newHolder() holder { return holder{ready: reachable} }

func TestGuarded(t *testing.T) {
	h := newHolder()
	if !h.ready() {
		t.Skip("cluster not reachable")
	}
	t.Log("assertion")
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})
	o := mustRun(t, root)
	if len(o.Sites) != 0 || !o.Pass {
		t.Fatalf("a func-valued field nothing fills with a probe is not a site; got %d site(s), pass=%v:\n%s",
			len(o.Sites), o.Pass, joined(o.Violations))
	}
}

// ---------------------------------------------------------------------------
// presentFn()() -- the written decision
// ---------------------------------------------------------------------------

func TestAValueReturnedByASoleReturnIsRead(t *testing.T) {
	// One unconditional return IS knowing where the value came from.
	wantAbsentSite(t, funcValueTree(t, `
func presentFn() func() bool { return cliPresent }`,
		"\tif !presentFn()() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestAValueReturnedByTwoReturnsIsUnresolvedRatherThanSilent(t *testing.T) {
	// A second return is a guess about which one ran, and the declared limit is
	// that this gate does not guess -- out loud, in red.
	wantUnresolvedSite(t, funcValueTree(t, `
func presentFn(fast bool) func() bool {
	if fast {
		return func() bool { return true }
	}
	return cliPresent
}`,
		"\tif !presentFn(false)() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"),
		"value returned by a call")
}

func TestAProducerThatTouchesNoFilesystemIsNotASite(t *testing.T) {
	src := `package unit

import "testing"

func reachable() bool { return true }

func readyFn() func() bool { return reachable }

func TestGuarded(t *testing.T) {
	if !readyFn()() {
		t.Skip("cluster not reachable")
	}
	t.Log("assertion")
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})
	if o := mustRun(t, root); len(o.Sites) != 0 || !o.Pass {
		t.Fatalf("a factory returning a non-filesystem predicate is not a site; got %d, pass=%v",
			len(o.Sites), o.Pass)
	}
}

// ---------------------------------------------------------------------------
// the reopening attempts -- all three walked past the first fix in silence
// ---------------------------------------------------------------------------

func TestAPredicateHandedToAHelperAsAFuncValueIsReadAtTheCall(t *testing.T) {
	// Measured against the first B-189 build on 2026-09-05: the six original
	// spellings were red and this one passed, silently. The guard is the
	// CALLER's, so it is reported at the call and not at the helper.
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
	if !present() {
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

func TestAnEntryOfATableOfPredicatesIsRead(t *testing.T) {
	wantAbsentSite(t, funcValueTree(t, `
var probes = map[string]func() bool{"cli": cliPresent}`,
		"\tif !probes[\"cli\"]() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"))
}

func TestATableEntryWithAnUnreadableKeyIsUnresolvedRatherThanSilent(t *testing.T) {
	wantUnresolvedSite(t, funcValueTree(t, `
var probes = map[string]func() bool{"cli": cliPresent}`,
		"\tk := \"c\" + \"li\"\n\tif !probes[k]() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"),
		"table of predicates")
}

func TestANameReboundToAnotherFuncValueIsUnresolvedRatherThanSilent(t *testing.T) {
	// `var present = alwaysTrue` swapped in an init() runs the SECOND value, so
	// reading the initializer answers about a function that never ran. Refusing
	// costs an UNRESOLVED the pin can override; trusting it was the silence.
	wantUnresolvedSite(t, funcValueTree(t, `
func alwaysTrue() bool { return true }

var present = alwaysTrue

func init() { present = cliPresent }`,
		"\tif !present() {\n\t\tt.Skip(\"cli/ not present\")\n\t}"),
		"rebound to another func value")
}

// ---------------------------------------------------------------------------
// the limits that stay
// ---------------------------------------------------------------------------

func TestABoolValueGuardIsStillOutOfReach(t *testing.T) {
	// A bool has no body to read. This one is honestly out of reach, and the
	// SCOPE says so rather than implying coverage it does not have -- which is
	// the mistake B-189 was opened for.
	src := `package unit

import "testing"

type harness struct{ havePath bool }

func TestGuarded(t *testing.T) {
	h := harness{}
	if !h.havePath {
		t.Skip("no path")
	}
	t.Log("assertion")
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})
	if o := mustRun(t, root); len(o.Sites) != 0 || !o.Pass {
		t.Fatalf("a bool guard has no body to read and is a declared limit; got %d site(s), pass=%v",
			len(o.Sites), o.Pass)
	}
}
