package testskipabsentpath

import "testing"

// A `:=` RANGE DECLARES A LOOP-SCOPED VARIABLE, AND THE FIX LET IT ESCAPE.
//
// `bindRangeValue` bound the range's value name into the frame permanently. Go scopes
// it to the loop, so after the loop the outer name still holds what it held before —
// and a guard reading it got the range binding instead, failed the non-probe bar, and
// went SILENT.
//
// Measured 2026-09-06 by an audit on the real tree: BEFORE this fix the guard was
// reported (14 path-guarded, 3 violations, naming the right path); AFTER it was 13
// path-guarded, 2 violations, and the live guard had disappeared. A new silence,
// introduced by the change whose whole subject is that silences are the defect.

func TestARangeDeclarationDoesNotAnswerForTheNameAfterTheLoop(t *testing.T) {
	src := `package unit

import (
	"os"
	"testing"
)

func cliPresent() bool {
	_, err := os.Stat("../../../cli/src")
	return err == nil
}

func alwaysTrue() bool { return true }

func TestGuarded(t *testing.T) {
	f := cliPresent
	for _, f := range []func() bool{alwaysTrue} {
		_ = f
	}
	if !f() {
		t.Skip("cli/ not present")
	}
	t.Fatal("dead assertion")
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})

	o := mustRun(t, root)
	if len(o.Sites) != 1 {
		t.Fatalf("Go reads cliPresent after the loop; the guard is a site. Got %d: %+v",
			len(o.Sites), o.Sites)
	}
	if o.Pass {
		t.Fatalf("the path is absent and undeclared: %s", joined(o.Violations))
	}
}

func TestARangeAssignmentToAnOuterNameIsStillUnresolved(t *testing.T) {
	// The other direction. An `=` range assigns to a name the frame already holds,
	// and that binding DOES outlive the loop — a range runs zero times over an
	// empty container, so which value a later guard reads is undecidable. That is
	// UNRESOLVED, not a guess and not a silence.
	src := `package unit

import (
	"os"
	"testing"
)

func cliPresent() bool {
	_, err := os.Stat("../../../cli/src")
	return err == nil
}

func alwaysTrue() bool { return true }

func TestGuarded(t *testing.T) {
	var f func() bool
	f = cliPresent
	for _, f = range []func() bool{alwaysTrue} {
		_ = f
	}
	if !f() {
		t.Skip("cli/ not present")
	}
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})

	o := mustRun(t, root)
	if len(o.Sites) != 1 {
		t.Fatalf("want one site, got %d: %+v", len(o.Sites), o.Sites)
	}
	if o.Sites[0].Verdict == Present || o.Sites[0].Verdict == Absent {
		t.Fatalf("which value the guard reads is undecidable here; a confident %v is a guess",
			o.Sites[0].Verdict)
	}
}

func TestAGuardInsideTheLoopStillSeesTheRangeValue(t *testing.T) {
	// Scoping must not mean unbinding: inside the body, the name IS the range value.
	src := `package unit

import (
	"os"
	"testing"
)

func cliPresent() bool {
	_, err := os.Stat("../../../cli/src")
	return err == nil
}

func TestGuarded(t *testing.T) {
	for _, f := range []func() bool{cliPresent} {
		if !f() {
			t.Skip("cli/ not present")
		}
	}
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})

	o := mustRun(t, root)
	if len(o.Sites) != 1 {
		t.Fatalf("a guard inside the loop is a site; got %d: %+v", len(o.Sites), o.Sites)
	}
}
