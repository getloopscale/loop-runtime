// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package alertrunbooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

func writeRule(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "infra", "helm", "monitoring", "manifests")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ruleWith(url string) string {
	return `spec:
  groups:
    - name: x
      rules:
        - alert: LoopZeroSomethingBroke
          expr: up == 0
          annotations:
            summary: "something"
            runbook_url: "` + url + `"
`
}

// The gate measured the annotation's PRESENCE, not the runbook's existence. Measured on
// 2026-08-19: of the 7 URLs in the tree, SIX led nowhere — and all seven counted as
// satisfied. An alert that fires at 3am sending the operator to a 404 is worse than an
// alert without a link, because the link makes a promise.
func TestFlagsOwnRepoPathThatDoesNotExist(t *testing.T) {
	root := t.TempDir()
	writeRule(t, root, "prometheusrule-test.yaml", ruleWith(
		"https://github.com/getloopscale/loop-runtime/blob/main/docs/runbooks/does-not-exist.md"))

	out := Check(Config{Root: root})
	if len(out.DeadLinks) != 1 {
		t.Fatalf("wanted 1 dead link, got %d: %+v", len(out.DeadLinks), out.DeadLinks)
	}
	if !strings.Contains(out.DeadLinks[0].Reason, "does not exist") {
		t.Errorf("the reason does not say the file does not exist: %+v", out.DeadLinks[0])
	}
	if out.Pass {
		t.Error("passed with a runbook pointing at a nonexistent file in the own repo")
	}
}

// The counterpart: a path that EXISTS must not be flagged, otherwise the gate becomes
// noise and people learn to work around it.
func TestDoesNotFlagOwnRepoPathThatExists(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "runbooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "runbooks", "exists.md"), []byte("# ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRule(t, root, "prometheusrule-test.yaml", ruleWith(
		"https://github.com/getloopscale/loop-runtime/blob/main/docs/runbooks/exists.md"))

	out := Check(Config{Root: root})
	if len(out.DeadLinks) != 0 {
		t.Errorf("flagged a runbook that exists on disk: %+v", out.DeadLinks)
	}
}

// A different ORG is NOT a dead link — and this test replaces one that asserted the
// opposite.
//
// The version from a few hours earlier treated any org != `getloopscale` as dead "by
// construction". Measured afterwards, with a credential that can see: `getloopscale/loop-runtime`
// and `getloopscale/loop-runtime` are **the same repository**, the first redirecting to the second,
// and the files resolve in both. The rule would have produced false positives as soon as
// the gate's scope grew (B-164).
//
// How the error got in: I extracted truncated URLs from the output, INVENTED the file
// names they seemed to have, probed THOSE names through the API and got Not Found. The
// real names were different and resolve. Probing what is assumed instead of what the file
// says is evidence fabrication with an extra step.
//
// The rule that survives the measurement is by REPOSITORY: if the URL points at this repo
// (`loop-runtime`), the path is checked on disk, whatever the org; if it points at another repo,
// the path does not exist in this tree by definition, and asserting it does not exist in
// THAT one would require the network — so it is counted as unverifiable, not accused.
func TestDifferentOrgOnSameRepoIsStillCheckedOnDisk(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "runbooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "runbooks", "exists.md"), []byte("# ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRule(t, root, "prometheusrule-test.yaml", ruleWith(
		"https://github.com/getloopscaledev/loop-runtime/blob/develop/docs/runbooks/exists.md"))

	out := Check(Config{Root: root})
	if len(out.DeadLinks) != 0 {
		t.Errorf("flagged as dead a link whose file EXISTS, only because the org in the path differs. "+
			"`getloopscaledev/loop-runtime` redirects to `getloopscale/loop-runtime` — it is the same repository: %+v", out.DeadLinks)
	}
	if out.UnverifiableLinks != 0 {
		t.Errorf("counted as unverifiable a link to this same repo, which can be checked on disk")
	}
}

// A different repository is UNVERIFIABLE, not dead: the path does not exist in this tree
// by definition, and saying it does not exist in ITS tree would require the network.
func TestOtherRepositoryCountsAsUnverifiable(t *testing.T) {
	root := t.TempDir()
	writeRule(t, root, "prometheusrule-test.yaml", ruleWith(
		"https://github.com/getloopscale/other-repo/blob/main/docs/x.md"))

	out := Check(Config{Root: root})
	if len(out.DeadLinks) != 0 {
		t.Errorf("flagged as dead a link to ANOTHER repository, with no way of knowing: %+v", out.DeadLinks)
	}
	if out.UnverifiableLinks != 1 {
		t.Errorf("wanted 1 unverifiable, got %d", out.UnverifiableLinks)
	}
}

// Traversal must not reach a file outside the root. The mutant is a REAL file planted
// OUTSIDE — without the handling, `Join(root, "../target.md")` would resolve to it and the
// gate would say the runbook exists. With the handling, the `..` is resolved against the
// root and becomes an ordinary relative path that does not exist.
//
// This test replaces a containment check I wrote and removed: after the `Clean`, it had
// no way to fire. An unreachable guard is dead code that reads as protection.
func TestTraversalDoesNotReachFileOutsideRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// The traversal target, existing and outside the root.
	if err := os.WriteFile(filepath.Join(base, "target.md"), []byte("# outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRule(t, root, "prometheusrule-test.yaml", ruleWith(
		"https://github.com/getloopscale/loop-runtime/blob/main/../target.md"))

	out := Check(Config{Root: root})
	if len(out.DeadLinks) != 1 {
		t.Fatalf("the traversal reached the outside file and the link passed as alive: %+v", out.DeadLinks)
	}
	if !strings.Contains(out.DeadLinks[0].Reason, "does not exist") {
		t.Errorf("unexpected reason: %+v", out.DeadLinks[0])
	}
}

// What the gate CANNOT verify without the network stays counted, not hidden. An external
// URL may be alive or dead; asserting either hermetically would be inventing. Counting is
// honest — and the number is what makes someone check.
func TestExternalURLCountsAsUnverifiableInsteadOfSilence(t *testing.T) {
	root := t.TempDir()
	writeRule(t, root, "prometheusrule-test.yaml", ruleWith("https://docs.getloopscale.com/runbooks/something"))

	out := Check(Config{Root: root})
	if len(out.DeadLinks) != 0 {
		t.Errorf("flagged an external URL as dead without being able to know: %+v", out.DeadLinks)
	}
	if out.UnverifiableLinks != 1 {
		t.Errorf("wanted 1 unverifiable link counted, got %d", out.UnverifiableLinks)
	}
}

// The real population (`rules/testing.md` § 4.2). A mutant proves the detector fires;
// only this says what it finds in what exists.
func TestRealPopulationOfRunbookLinks(t *testing.T) {
	out := Check(Config{Root: qtestutil.Root(t)})
	// Count the CHECKED ones, not the problematic ones. The first version summed dead +
	// unverifiable and treated zero as "reached nothing" — but zero is ALSO the result of
	// every link being good, which is the current state of the tree. Confusing "found
	// nothing" with "nothing is wrong" is the exact shape this gate fights, and it was in
	// the test that was supposed to prove it.
	if out.LinksChecked == 0 {
		t.Fatal("no runbook link reached — green over nothing is not green")
	}
	if out.DirsScanned == 0 {
		t.Fatal("no rules directory scanned")
	}
	t.Logf("population: %d directory(ies), %d link(s) checked, %d dead, %d unverifiable",
		out.DirsScanned, out.LinksChecked, len(out.DeadLinks), out.UnverifiableLinks)
	for _, d := range out.DeadLinks {
		t.Logf("  dead: %s [%s] %s — %s", d.File, d.Alert, d.URL, d.Reason)
	}
}
