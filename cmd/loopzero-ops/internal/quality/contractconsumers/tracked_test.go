// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package contractconsumers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

// A TRACKED PATH that does not exist has to show up, for the same reason an absent
// consumer root already does — and this file protected the two halves asymmetrically.
//
// consumerRoots got its missing-roots guard on 2026-08-19 (B-100/B-040) because `git grep -- a b c`
// silently ignores a pathspec that matches nothing. trackedFiles is handed over in exactly
// the same way — `git diff --unified=0 base..head -- <list>` in collectDroppedJSONTags and
// `git grep -cE ... -- <list>` in structsDeclaringTagAt — and had no guard at all. Measured
// in this tree: `git diff` with a nonexistent pathspec exits 0, without a line on stderr.
//
// The unprotected half is the one that decides WHICH tags the gate looks at. An absent
// consumer root shrinks where it hunts for the reader; an absent tracked path means the
// gate never sees the field drop — the verdict becomes PASS over an empty population,
// which is the `PASS — 0` shape this ecosystem has already paid to learn.
//
// And the trigger is concrete, not hypothetical: trackedFiles[0] is
// api/internal/domain/types.go, and B-057 exists to rename that directory to
// api/internal/ports. On rename day this gate loses 93 `json:` tags (measured 2026-09-05),
// among them ProjectSummary (types.go:247), which is *the struct from the 2026-06-08
// walkthrough that made the gate exist*, cited in the package's own doc. The gate would
// go on saying PASS.
func TestMissingTrackedPathSurfacesInsteadOfVanishing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "loopzero") // sibling roots resolve inside the temp dir
	// A tree where the consumer roots exist but NO tracked path does: isolates the half
	// under test from the signal the other half already emits.
	for _, r := range consumerRoots {
		if err := os.MkdirAll(filepath.Join(root, r.Repo, r.Path), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out == nil {
		t.Fatal("Check returned a nil outcome")
	}
	if len(out.MissingTracked) == 0 {
		t.Fatalf("no tracked path reported missing, and the test tree contains none of the "+
			"%d declared (%v) — the gate carried on as though it had swept the whole "+
			"contract surface", len(trackedFiles), trackedFiles)
	}
	// Every declared path, plus the handler surface, which an empty tree does not
	// hold either: discovered paths that discover nothing are reported, not dropped.
	if want := len(trackedFiles) + 1; len(out.MissingTracked) != want {
		t.Errorf("an empty tree should report all %d tracked paths missing, reported %d: %v",
			want, len(out.MissingTracked), out.MissingTracked)
	}
}

// The contrapositive: against the REAL tree, no tracked path may be reported missing.
// Without this, MissingTracked could be a list that always returns everything — green in
// the test above and useless in life.
func TestTrackedPathsExistInTheRealTree(t *testing.T) {
	root := qtestutil.MustRepoRoot()

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(trackedFiles) == 0 {
		t.Fatal("no tracked path declared — green over nothing is not green")
	}
	if len(out.MissingTracked) > 0 {
		t.Fatalf("%d of %d tracked path(s) do NOT exist in this tree: %v. The pathspec is "+
			"silently ignored by git diff/grep, so this absence is contract coverage lost "+
			"with no change of verdict.",
			len(out.MissingTracked), len(trackedFiles), out.MissingTracked)
	}
	t.Logf("tracked paths declared: %d; missing in the real tree: 0 %v", len(trackedFiles), trackedFiles)
}

// The anchor tying this gate to B-057. The rename of api/internal/domain to
// api/internal/ports is a known, open DoD bullet; when it happens, whoever performs it
// must be forced to look here.
//
// This test does NOT demand the path stay `domain` forever — it demands the declared path
// exist. After the rename, pointing trackedFiles at `ports` turns it green again; NOT
// updating it leaves it red. Which is exactly the difference between the property
// travelling with the rename and dying in it.
func TestDeclaredContractSurfaceCoversThePortSink(t *testing.T) {
	root := qtestutil.MustRepoRoot()

	var portFile string
	for _, f := range trackedFiles {
		if strings.HasSuffix(f, ".go") {
			portFile = f
			break
		}
	}
	if portFile == "" {
		t.Fatal("no individual .go file in trackedFiles — the port sink left the tracked " +
			"surface; if that was deliberate, this test is the place to say where it went")
	}
	if _, err := os.Stat(filepath.Join(root, portFile)); err != nil {
		t.Fatalf("trackedFiles declares %q and it does not exist: %v", portFile, err)
	}

	// The struct named in the package doc as the 2026-06-08 regression that made the gate
	// exist. If it leaves this file, the tracked surface has to follow it.
	content, err := os.ReadFile(filepath.Join(root, portFile))
	if err != nil {
		t.Fatalf("read %s: %v", portFile, err)
	}
	if !strings.Contains(string(content), "type ProjectSummary struct") {
		t.Fatalf("%s no longer declares ProjectSummary — the struct behind the regression "+
			"this gate was built to catch moved house and trackedFiles did not follow", portFile)
	}
}
