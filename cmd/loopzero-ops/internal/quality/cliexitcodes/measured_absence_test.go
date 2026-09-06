package cliexitcodes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A LINE THAT IS STILL TRUE MUST NOT BE TOLD IT IS STALE.
//
// B-190 made the gate stat every pinned SUBJECT, which is the measurement the file's
// header had been promising. The measurement was then thrown away in the branch where
// a CLI source tree IS found: every declared-absent line was told "the absence closed;
// delete that line", including a line whose subject this same run had just measured
// and NOT found.
//
// Measured by an audit on 2026-09-05 with a pin holding two lines — one present, one
// absent — and a tree present:
//
//	docs    is declared ABSENT here, but it EXISTS at .../docs        (true)
//	foo/bar is declared ABSENT here, but a CLI source tree EXISTS ...  (false)
//
// The second sentence is about a subject that is still absent, printed by the gate
// whose whole purpose in B-190 was to stop printing false sentences about the
// filesystem. Two different facts — "this line's subject came back" and "some tree
// was found" — were being reported with one message.

func TestALineWhoseSubjectIsStillAbsentIsNotToldTheAbsenceClosed(t *testing.T) {
	root := t.TempDir()
	// A real CLI tree, so the run takes the tree-found branch.
	write(t, root, "cli/src/main.ts", "process.exit(0)\n")
	// And a pin line naming a subject that is genuinely not here.
	writePin(t, root, "foo/bar", "still absent, and this run measured it")

	o := check(t, Config{RepoRoot: root})

	for _, v := range o.Violations {
		if strings.Contains(v, "foo/bar") && strings.Contains(v, "the absence closed") {
			t.Fatalf("foo/bar is absent and the run measured it; it must not be told "+
				"its absence closed:\n  %s", v)
		}
	}
}

func TestAMeasuredAbsenceIsCarriedEvenWhenATreeWasFound(t *testing.T) {
	// The field the header promises: "the pinned lines whose Subject this run
	// stat'd and did NOT find". It was populated in one branch of two.
	root := t.TempDir()
	write(t, root, "cli/src/main.ts", "process.exit(0)\n")
	writePin(t, root, "foo/bar", "still absent")

	o := check(t, Config{RepoRoot: root})

	if len(o.TrackedAbsences) != 1 {
		t.Fatalf("a subject measured absent must be carried whichever branch ran; got %+v",
			o.TrackedAbsences)
	}
	if o.TrackedAbsences[0].Subject != "foo/bar" {
		t.Fatalf("wrong subject carried: %+v", o.TrackedAbsences[0])
	}
}

func TestASubjectThatCameBackIsStillReportedAsClosed(t *testing.T) {
	// The other direction, so the fix is a distinction and not a blanket silence.
	root := t.TempDir()
	write(t, root, "cli/src/main.ts", "process.exit(0)\n")
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writePin(t, root, "docs", "declared absent, but it is right here")

	o := check(t, Config{RepoRoot: root})

	var named bool
	for _, v := range o.Violations {
		if strings.Contains(v, "docs") && strings.Contains(v, "EXISTS") {
			named = true
		}
	}
	if !named {
		t.Fatalf("a pinned subject that exists must still fail, naming it: %v", o.Violations)
	}
}
