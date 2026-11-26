// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package testskipabsentpath

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// B-186: the level below the level B-183 closed
//
// B-183 taught this gate to read a probe moved ONE level into a same-package
// predicate. An independent audit moved it one level further -- two functions,
// the same refactor twice -- and the gate printed PASS with exit 0. The failure
// was not the depth; it was the SILENCE. The package doc states a floor: a
// predicate that reaches a probe but whose answer cannot be followed is
// UNRESOLVED and FAILS. The delegating shape reached a probe and was not
// reported at all, because reachability was read on the predicate's own body.
//
// Every test here fixes one shape at the verdict it must produce. There is no
// depth at which the answer may be "nothing to report".
// ---------------------------------------------------------------------------

// nemesisDelegateSrc is the evidence block of B-186, verbatim: the two-function
// evasion an audit measured against this gate on 2026-09-05. It is the oracle.
const nemesisDelegateSrc = `package config

import (
	"os"
	"testing"
)

func nemesisDirExists(p string) bool { _, err := os.Stat(p); return err == nil }
func nemesisCliSrcPresent() bool     { return nemesisDirExists("../../../cli/src") }

func TestNemesisEvadeViaHelper(t *testing.T) {
	if !nemesisCliSrcPresent() {
		t.Skip("cli/ not present")
	}
	t.Fatal("dead assertion")
}
`

func TestPredicateDelegatingToAnotherPredicateIsDetected(t *testing.T) {
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":        pinHeader,
		"infra/tests/config/a_test.go": nemesisDelegateSrc,
	})

	o := mustRun(t, root)

	if o.Pass {
		t.Fatal("`present() { return dirExists(p) }` is the same guard as the os.Stat it wraps, " +
			"two functions down instead of one; passing it is the refactor that reopens the class")
	}
	v := joined(o.Violations)
	// File, func and path -- the three things the reader needs to act without
	// re-deriving the finding from the gate's source.
	for _, want := range []string{
		"infra/tests/config/a_test.go",
		"TestNemesisEvadeViaHelper",
		"cli/src",
		"ABSENT",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("the violation must name %q; got:\n%s", want, v)
		}
	}
	if len(o.Sites) != 1 {
		t.Fatalf("want exactly 1 site, got %d: %+v", len(o.Sites), o.Sites)
	}
	if o.Sites[0].Verdict != Absent {
		t.Errorf("the path is genuinely absent, so the verdict is Absent (not Unresolved): got %v",
			o.Sites[0].Verdict)
	}
	// The path must come from the INNERMOST predicate's scope, with the caller's
	// literal carried in. Resolving `p` anywhere else answers about a name that
	// scope never bound.
	if o.Sites[0].Path != "cli/src" {
		t.Errorf("the resolved path must be the delegate's probed path; got %q", o.Sites[0].Path)
	}
}

func TestDelegationChainNamesEveryHop(t *testing.T) {
	// Four levels, with the path handed down as an argument at each. The
	// message has to name the chain: a reader who is told only the outermost
	// predicate has to open three more files to find the probe.
	src := `package unit

import (
	"os"
	"testing"
)

func l4(p string) bool { _, err := os.Stat(p); return err == nil }
func l3(p string) bool { return l4(p) }
func l2(p string) bool { return l3(p) }
func l1() bool         { return l2("../../../cli/src") }

func TestGuarded(t *testing.T) {
	if !l1() {
		t.Skip("gone")
	}
	t.Fatal("dead assertion")
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})

	o := mustRun(t, root)
	if o.Pass {
		t.Fatalf("four levels of delegation is still one guard on one path; sites=%+v", o.Sites)
	}
	if len(o.Sites) != 1 || o.Sites[0].Path != "cli/src" {
		t.Fatalf("want one site on cli/src, got %+v", o.Sites)
	}
	for _, hop := range []string{"l1()", "l2()", "l3()", "l4()"} {
		if !strings.Contains(o.Sites[0].Expr, hop) {
			t.Errorf("the rendered guard must name %s so the reader can walk to the probe; got %q",
				hop, o.Sites[0].Expr)
		}
	}
}

func TestCyclicPredicateIsUnresolvedAndNotSilent(t *testing.T) {
	// The declared behaviour at the depth bound. Following the answer costs
	// maxResolveDepth hops; a cycle never spends fewer. What must NOT happen is
	// the gate going quiet -- an unbounded resolver would hang and a silent one
	// would hand back the evasion, so the bound buys a verdict instead.
	src := `package unit

import (
	"os"
	"testing"
)

func ping() bool { return pong() }

func pong() bool {
	_, err := os.Stat("../../../cli/src")
	_ = err
	return ping()
}

func TestGuarded(t *testing.T) {
	if !ping() {
		t.Skip("gone")
	}
	t.Fatal("dead assertion")
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})

	o := mustRun(t, root)
	if o.Pass {
		t.Fatal("a predicate that reaches a probe and answers through a cycle must FAIL as UNRESOLVED: " +
			"the depth bound may cost a verdict, never a silence")
	}
	if len(o.Sites) != 1 || o.Sites[0].Verdict != Unresolved {
		t.Fatalf("want one Unresolved site, got %+v", o.Sites)
	}
	v := joined(o.Violations)
	if !strings.Contains(v, "could not resolve") {
		t.Errorf("the violation must say the gate could not resolve it; got:\n%s", v)
	}
	if !strings.Contains(v, "cyclic") {
		t.Errorf("the message must name the cycle rather than printing the loop until the bound runs "+
			"out; got:\n%s", v)
	}
	if n := strings.Count(o.Sites[0].Expr, "ping()"); n > 3 {
		t.Errorf("the rendered chain must collapse the cycle, not repeat it %d times: %q",
			n, o.Sites[0].Expr)
	}
}

func TestPredicateReturningBoolAndErrorIsDetected(t *testing.T) {
	// The second shape B-186 declared. `returnsBool` demanded exactly one
	// result, so this was rejected BEFORE the probe check ran and the UNRESOLVED
	// floor never got a chance. Go gives a (bool, error) predicate exactly one
	// spelling in a condition -- bind it, then test the bound local.
	src := `package unit

import (
	"os"
	"testing"
)

func cliSrcPresent() (bool, error) {
	if _, err := os.Stat("../../../cli/src"); err != nil {
		return false, err
	}
	return true, nil
}

func TestGuarded(t *testing.T) {
	if ok, _ := cliSrcPresent(); !ok {
		t.Skip("gone")
	}
	t.Fatal("dead assertion")
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})

	o := mustRun(t, root)
	if o.Pass {
		t.Fatalf("an error result beside the answer does not change the guard; sites=%+v", o.Sites)
	}
	if len(o.Sites) != 1 || o.Sites[0].Path != "cli/src" || o.Sites[0].Verdict != Absent {
		t.Fatalf("want one Absent site on cli/src, got %+v", o.Sites)
	}
}

func TestBoolAndErrorPredicateBoundOutsideTheIfIsDetected(t *testing.T) {
	// The same shape with the bind on its own statement. Reading only the if's
	// init would close one spelling and leave the other open -- which is the
	// mistake this whole item exists to stop repeating.
	src := `package unit

import (
	"os"
	"testing"
)

func cliSrcPresent() (bool, error) {
	_, err := os.Stat("../../../cli/src")
	return err == nil, err
}

func TestGuarded(t *testing.T) {
	ok, _ := cliSrcPresent()
	if !ok {
		t.Skip("gone")
	}
	t.Fatal("dead assertion")
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})

	o := mustRun(t, root)
	if o.Pass {
		t.Fatalf("binding the predicate a line earlier is the same guard; sites=%+v", o.Sites)
	}
	if len(o.Sites) != 1 || o.Sites[0].Path != "cli/src" {
		t.Fatalf("want one site on cli/src, got %+v", o.Sites)
	}
}

func TestSkipInTheElseArmOfAPresencePredicateIsDetected(t *testing.T) {
	// The third shape. `if present() { t.Fatal } else { t.Skip }` fires the skip
	// exactly on absence, and reading only the then-arm made it silent while the
	// doc declared the OPPOSITE polarity as the deliberate non-site.
	src := `package unit

import (
	"os"
	"testing"
)

func cliSrcPresent() bool {
	_, err := os.Stat("../../../cli/src")
	return err == nil
}

func TestGuarded(t *testing.T) {
	if cliSrcPresent() {
		t.Fatal("dead assertion")
	} else {
		t.Skip("gone")
	}
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})

	o := mustRun(t, root)
	if o.Pass {
		t.Fatalf("a skip in the else arm of a presence predicate fires on absence like any other; "+
			"sites=%+v", o.Sites)
	}
	if len(o.Sites) != 1 || o.Sites[0].Path != "cli/src" || o.Sites[0].Verdict != Absent {
		t.Fatalf("want one Absent site on cli/src, got %+v", o.Sites)
	}
	// The reported line is the SKIP's, not the if's: the reader is being sent to
	// the statement that fires.
	if o.Sites[0].Line != 17 {
		t.Errorf("the site must point at the skip in the else arm (line 17), got line %d",
			o.Sites[0].Line)
	}
}

func TestSkipInlineInTheElseArmIsDetected(t *testing.T) {
	// The same arm with the probe written inline. This half is PRE-EXISTING --
	// it behaved identically before B-186 -- and it is fixed here because it is
	// the same rule, not because the audit reached it through a predicate.
	src := `package unit

import (
	"os"
	"testing"
)

func TestGuarded(t *testing.T) {
	if _, err := os.Stat("../../../cli/src"); err == nil {
		t.Fatal("dead assertion")
	} else {
		t.Skip("gone")
	}
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})

	o := mustRun(t, root)
	if o.Pass {
		t.Fatalf("`err == nil` in the then-arm puts the absence skip in the else arm; sites=%+v", o.Sites)
	}
	if len(o.Sites) != 1 || o.Sites[0].Path != "cli/src" {
		t.Fatalf("want one site on cli/src, got %+v", o.Sites)
	}
}

func TestSkipOnPresenceIsNotASiteInEitherArm(t *testing.T) {
	// The control for the change above, and the polarity the doc still declares
	// out of scope: a skip that fires when the path IS there cannot be caused by
	// a missing path. Reading both arms must not turn that into a false site --
	// which is the failure mode of every "read more" fix.
	src := `package unit

import (
	"os"
	"testing"
)

func migrated() bool {
	_, err := os.Stat("../../../cli/src")
	return err == nil
}

func TestThenArm(t *testing.T) {
	if migrated() {
		t.Skip("already migrated")
	}
}

func TestElseArm(t *testing.T) {
	if !migrated() {
		t.Log("absent, carry on")
	} else {
		t.Skip("already migrated")
	}
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})

	o := mustRun(t, root)
	if len(o.Sites) != 0 {
		t.Fatalf("a skip that fires on PRESENCE is not an always-fires skip, in either arm; got %+v",
			o.Sites)
	}
}

func TestDelegationToANonFilesystemPredicateIsNotASite(t *testing.T) {
	// The control for transitive reachability. Following calls must not make the
	// gate opinionated about predicates that never touch the filesystem: a
	// scanner that reports everything is as useless as one that reports nothing.
	src := `package unit

import (
	"os"
	"testing"
)

func envSet(k string) bool { return os.Getenv(k) != "" }
func clusterReachable() bool { return envSet("KUBECONFIG") }

func TestGuarded(t *testing.T) {
	if !clusterReachable() {
		t.Skip("no cluster")
	}
	t.Log("live assertion")
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
		"infra/tests/unit/b_test.go": guardedTest("unit", "TestLive", "../../../docs/live.md"),
		"docs/live.md":               "here\n",
	})

	o := mustRun(t, root)
	for _, s := range o.Sites {
		if s.Func == "TestGuarded" {
			t.Fatalf("a predicate that delegates only to env reads is none of this gate's business; got %+v", s)
		}
	}
	if !o.Pass {
		t.Errorf("nothing here should fail; violations:\n%s", joined(o.Violations))
	}
}
