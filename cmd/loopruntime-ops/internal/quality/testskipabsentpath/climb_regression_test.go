package testskipabsentpath

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// A path that climbs OUT of the tree and back IN under the repository's own
// directory name names a file inside this repository, and must be judged like any
// other. The `Outside` verdict decided that by string shape -- anything cleaning to
// a leading `../` was declared not-our-problem -- so this spelling escaped.
//
// Found by an independent audit on 2026-09-05, which measured that the parent
// binary FAILED on both spellings of the same absent file while the new one judged
// only the honest spelling. Fixing the fabricated-path defect had traded it for a
// new silence, and the silence is reachable by anyone who wants it.

func TestAClimbThatReturnsIntoTheTreeIsStillJudged(t *testing.T) {
	root := tree(t, map[string]string{
		"infra/quality/pin.txt": pinHeader,
		// Placeholder so the scan root exists before the real file is written.
		"infra/tests/unit/keep_test.go": "package unit\n",
	})
	base := filepath.Base(root)

	// From infra/tests/unit: three levels up is the root, four is its parent.
	// So this leaves the tree and comes straight back into it by name, landing
	// on exactly the same absent file as "../../scripts/gone.sh".
	src := fmt.Sprintf(`package unit

import (
	"os"
	"testing"
)

const gone = "../../../../%s/infra/scripts/gone.sh"

func TestGuarded(t *testing.T) {
	if _, err := os.Stat(gone); err != nil {
		t.Skip("script not here")
	}
	t.Fatal("dead assertion")
}
`, base)
	if err := os.WriteFile(filepath.Join(root, "infra", "tests", "unit", "a_test.go"),
		[]byte(src), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	o := mustRun(t, root)
	if len(o.Sites) != 1 {
		t.Fatalf("want one site, got %+v", o.Sites)
	}
	if o.Sites[0].Verdict == Outside {
		t.Fatalf("the path resolves to %s/infra/scripts/gone.sh, which is INSIDE this tree; "+
			"judging it by the leading `..` lets anyone opt out of the gate with a spelling",
			base)
	}
	if o.Sites[0].Verdict != Absent {
		t.Fatalf("want Absent, got %v (path %q)", o.Sites[0].Verdict, o.Sites[0].Path)
	}
	if o.Pass {
		t.Fatalf("an undeclared absent path must fail; violations:\n%s", joined(o.Violations))
	}
}

func TestTheTwoSpellingsOfOneAbsentFileAgree(t *testing.T) {
	// The property underneath the test above: how a path is WRITTEN must not
	// change what the gate decides about the file it names.
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":         pinHeader,
		"infra/tests/unit/keep_test.go": "package unit\n",
	})
	base := filepath.Base(root)

	write := func(name, path string) Site {
		t.Helper()
		src := fmt.Sprintf(`package unit

import (
	"os"
	"testing"
)

func TestGuarded(t *testing.T) {
	if _, err := os.Stat(%q); err != nil {
		t.Skip("script not here")
	}
}
`, path)
		full := filepath.Join(root, "infra", "tests", "unit", name)
		if err := os.WriteFile(full, []byte(src), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		o := mustRun(t, root)
		if len(o.Sites) != 1 {
			t.Fatalf("want one site for %s, got %+v", path, o.Sites)
		}
		s := o.Sites[0]
		if err := os.Remove(full); err != nil {
			t.Fatalf("cleanup: %v", err)
		}
		return s
	}

	direct := write("direct_test.go", "../../scripts/gone.sh")
	climbed := write("climbed_test.go", fmt.Sprintf("../../../../%s/infra/scripts/gone.sh", base))

	if direct.Verdict != climbed.Verdict {
		t.Fatalf("same file, two spellings, two answers: %q is %v but %q is %v",
			direct.Path, direct.Verdict, climbed.Path, climbed.Verdict)
	}
}

func TestAPathThatTrulyLeavesTheTreeIsStillNotJudged(t *testing.T) {
	// The fix must not undo what it is built on. A sibling checkout beside this
	// repository is genuinely not a claim about this repository -- that is the
	// live shape at api/internal/contract, probing ../../../../loopruntime-cloud/... .
	root := tree(t, map[string]string{
		"infra/quality/pin.txt": pinHeader,
		"infra/tests/unit/a_test.go": `package unit

import (
	"os"
	"testing"
)

const siblingSrc = "../../../../loopruntime-cloud/dashboard/src"

func TestGuarded(t *testing.T) {
	if _, err := os.Stat(siblingSrc); err != nil {
		t.Skip("sibling not cloned")
	}
}
`,
	})

	o := mustRun(t, root)
	if len(o.Sites) != 1 || o.Sites[0].Verdict != Outside {
		t.Fatalf("a genuine sibling path must still be Outside, got %+v", o.Sites)
	}
	if !o.Pass {
		t.Fatalf("and must not fail the gate; violations:\n%s", joined(o.Violations))
	}
}

func TestAnAbsolutePathThatPointsIntoThisTreeIsJudged(t *testing.T) {
	// Newly reachable: before the landing-place fix, every absolute literal was
	// Outside by shape. Now one that resolves inside the root is judged like any
	// other in-tree path -- so it has to be resolved as the absolute it is, not
	// joined onto the root a second time.
	//
	// Added because a mutant that did exactly that second join SURVIVED the first
	// pass: no test distinguished the two, since for relative paths filepath.Join
	// already cleans and the two are identical.
	root := tree(t, map[string]string{
		"infra/quality/pin.txt":         pinHeader,
		"infra/tests/unit/keep_test.go": "package unit\n",
		"infra/scripts/here.sh":         "#!/bin/sh\n",
	})
	src := fmt.Sprintf(`package unit

import (
	"os"
	"testing"
)

func TestGuarded(t *testing.T) {
	if _, err := os.Stat(%q); err != nil {
		t.Skip("script not here")
	}
}
`, filepath.Join(root, "infra", "scripts", "here.sh"))
	if err := os.WriteFile(filepath.Join(root, "infra", "tests", "unit", "a_test.go"),
		[]byte(src), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	o := mustRun(t, root)
	if len(o.Sites) != 1 {
		t.Fatalf("want one site, got %+v", o.Sites)
	}
	if o.Sites[0].Verdict != Present {
		t.Fatalf("the file is on disk inside the tree; want Present, got %v (path %q)",
			o.Sites[0].Verdict, o.Sites[0].Path)
	}
}
