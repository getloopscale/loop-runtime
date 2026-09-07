package testskipabsentpath

import "testing"

// `t.Run("name", func(t *testing.T){ ... })` is the dominant idiom for a Go test, and
// the walker never descended into a function literal. So every shape of this class
// was invisible inside a subtest — including the ORIGINAL spelling B-180 was opened
// for, an inline `os.Stat` guard with no predicate at all.
//
// Measured by an independent audit on 2026-09-05: the same guard, same path, same
// dead assertion beneath it, passed both the parent binary and the one that had just
// been written to close the class. With a live denominator — 6 of 146 skip calls in
// the scanned trees already sit inside function literals. Those six are bare skips
// today (B-184's class), so there was no live false negative, but the evasion was
// reachable on purpose, which is the bar the previous audit set.

func TestAnInlineGuardInsideASubtestIsASite(t *testing.T) {
	// The B-180 spelling, one closure down.
	src := `package unit

import (
	"os"
	"testing"
)

func TestOuter(t *testing.T) {
	t.Run("inner", func(t *testing.T) {
		if _, err := os.Stat("../../../cli/src"); err != nil {
			t.Skip("cli/ not present")
		}
		t.Fatal("dead assertion")
	})
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})

	o := mustRun(t, root)
	if len(o.Sites) != 1 {
		t.Fatalf("a guard inside t.Run is still a guard; got %d site(s): %+v", len(o.Sites), o.Sites)
	}
	if o.Pass {
		t.Fatalf("the path is absent and undeclared, so it must fail; violations:\n%s",
			joined(o.Violations))
	}
}

func TestAPredicateGuardInsideASubtestIsASite(t *testing.T) {
	// The B-186 spelling in the same position: both halves have to survive the
	// descent, or closing one shape just moves the hole.
	src := `package unit

import (
	"os"
	"testing"
)

func cliPresent() bool {
	info, err := os.Stat("../../../cli/src")
	return err == nil && info.IsDir()
}

func TestOuter(t *testing.T) {
	t.Run("inner", func(t *testing.T) {
		if !cliPresent() {
			t.Skip("cli/ not present")
		}
		t.Fatal("dead assertion")
	})
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})

	o := mustRun(t, root)
	if len(o.Sites) != 1 || o.Pass {
		t.Fatalf("want one failing site inside the closure; got %d, pass=%v", len(o.Sites), o.Pass)
	}
}

func TestAGuardInsideAnImmediatelyInvokedLiteralIsASite(t *testing.T) {
	src := `package unit

import (
	"os"
	"testing"
)

func TestOuter(t *testing.T) {
	func() {
		if _, err := os.Stat("../../../cli/src"); err != nil {
			t.Skip("cli/ not present")
		}
	}()
	t.Fatal("dead assertion")
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})

	if o := mustRun(t, root); len(o.Sites) != 1 || o.Pass {
		t.Fatalf("want one failing site in the literal; got %d, pass=%v", len(o.Sites), o.Pass)
	}
}

func TestTheSkipCountStillSeesSkipsInsideLiterals(t *testing.T) {
	// The floor "found no skip call at all" counts these. A closure the walker
	// cannot enter still contributes to the count, which is how a dead detector
	// could look alive.
	src := `package unit

import "testing"

func TestOuter(t *testing.T) {
	t.Run("inner", func(t *testing.T) {
		t.Skip("bare")
	})
}
`
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":      pinHeader,
		"infra/tests/unit/a_test.go": src,
	})

	o := mustRun(t, root)
	if o.SkipCalls < 1 {
		t.Fatalf("a bare skip inside a closure must still be counted; got %d", o.SkipCalls)
	}
	if !o.Pass {
		t.Fatalf("a BARE skip is B-184's class and not a site here; violations:\n%s",
			joined(o.Violations))
	}
}
