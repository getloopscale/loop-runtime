package cliexitcodes

import "testing"

// A MISSING PIN IS NOT A BROKEN SETUP HERE, AND THE REASON IS AN ASYMMETRY.
//
// Measured 2026-09-05: `--pin q/does-not-exist.txt` gives `PASS — 1 file(s)`. That
// looks like the found-but-unread family this gate was just fixed for, and it is not.
//
// The sibling gate (testskipabsentpath) DOES treat an unreadable pin as SETUP BROKEN
// and exit 5, and the difference is what each pin holds. There, a pin line is a live
// ANCHOR: losing the file silently drops tracked sites, so the gate goes quiet about
// things it was told to watch. Here, a pin line only ever ADDS a declaration —
// without the file, every absence is undeclared, which is the condition this gate
// FAILS on. Losing it can only make this gate stricter.
//
// A first draft of this file asserted the opposite and was wrong. The test kept is
// the one that holds: a pin that IS there must still be read, because that is the
// property an over-strict floor would have broken.

func TestThePinThatIsThereIsStillRead(t *testing.T) {
	root := t.TempDir()
	write(t, root, "cli/src/main.ts", "process.exit(0)\n")
	writePin(t, root, "foo/bar", "absent")

	o, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("a readable pin must not error: %v", err)
	}
	if len(o.TrackedAbsences) != 1 {
		t.Fatalf("the declared absence was not read: %+v", o.TrackedAbsences)
	}
}
