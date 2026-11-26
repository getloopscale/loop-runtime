// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package repoinvariants

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

// .github/scripts/assert_gate_observers_intact.py is what refuses, ON THIS
// MACHINE AND BEFORE THE PUSH, a commit that deletes or defangs the workflow
// that reports a disarmed delivery gate. These tests are what make its verdicts
// worth reading.
//
// WHY IT IS REACHED FROM HERE. `task quality:hermetic` runs `go test ./...` in
// this module and `quality:hermetic` is what the delivery gate invokes, so a Go
// test here runs before every push from a wired session. That is the ONE
// substrate where the deletion of `delivery-gate-armed.yml` can be reported with
// zero ungated pushes, because the deleting commit has not left the machine yet.
// It is deliberately not the only substrate: workflow-hygiene.yml runs the same
// script on GitHub, which is the half that still speaks when the local gate is
// itself the thing that died. A guard whose only runner is the thing it guards
// reports on itself, and that is the defect family this repository has now
// measured nine times.
//
// THE ORDER MATTERS AND SO DOES THE FIRST TEST. A checker that fails on
// everything and a checker that works are indistinguishable from a red run, and
// a checker that passes on everything is indistinguishable from a green one. So
// the anchor is asserted first — the script says PASS on this repository as it
// stands — and every test after it breaks exactly one thing and requires the
// script to name that thing, so a mutant that fails for an unrelated reason does
// not satisfy them.

func observersCheckPath(t *testing.T) string {
	t.Helper()
	root, err := qtestutil.RepoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	p := filepath.Join(root, ".github", "scripts", "assert_gate_observers_intact.py")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("this is the only thing that refuses a deleted gate-observer before the push; it must exist: %v", err)
	}
	return p
}

func runObserversCheck(t *testing.T, root string) (int, string) {
	t.Helper()
	cmd := exec.Command("python3", observersCheckPath(t), "--root", root)
	out, err := cmd.CombinedOutput()
	code := 0
	var exitErr *exec.ExitError
	if e, ok := err.(*exec.ExitError); ok {
		exitErr = e
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("could not run the observer-check at all: %v\n%s", err, out)
	}
	return code, string(out)
}

// observerTree copies the real `.github` files this check reads into a scratch
// root. They are COPIED rather than stubbed for the same reason armedTree copies
// the real hook: a stub would let this suite stay green while the real workflow
// had lost the trigger, which is the failure being defended against wearing a
// test's clothes.
func observerTree(t *testing.T) string {
	t.Helper()
	src, err := qtestutil.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, rel := range []string{
		".github/workflows/delivery-gate-armed.yml",
		".github/workflows/workflow-hygiene.yml",
		".github/scripts/assert_delivery_gate_armed.py",
	} {
		body, err := os.ReadFile(filepath.Join(src, rel))
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func rewriteObserver(t *testing.T, root, rel, old, replacement string) {
	t.Helper()
	p := filepath.Join(root, rel)
	body, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), old) {
		t.Fatalf("%s no longer contains %q, so this test is not mutating what it claims to mutate", rel, old)
	}
	out := strings.Replace(string(body), old, replacement, 1)
	if out == string(body) {
		t.Fatalf("the mutation of %s changed nothing", rel)
	}
	if err := os.WriteFile(p, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}

// THE ANCHOR. Without it every test below is satisfied by a script that always
// fails, and the suite would be green while the check was worthless.
func TestObserversCheck_PassesOnThisRepositoryAsItStands(t *testing.T) {
	root, err := qtestutil.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	code, out := runObserversCheck(t, root)
	if code != 0 {
		t.Fatalf("the observer-check fails on this repository — either an observer is genuinely gone or defanged, or the check is wrong.\nexit %d\n%s", code, out)
	}
	for _, want := range []string{"OBSERVER 1", "OBSERVER 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("the check passed without reporting %s. An observer it did not mention is one it did not check.\n%s", want, out)
		}
	}
}

// And the copied tree must pass too, or every mutant below fails for the wrong
// reason.
func TestObserversCheck_PassesOnTheCopiedTree(t *testing.T) {
	code, out := runObserversCheck(t, observerTree(t))
	if code != 0 {
		t.Fatalf("the intact fixture does not pass, so no mutant below is measuring what it claims to.\nexit %d\n%s", code, out)
	}
}

// THE FINDING THAT ORDERED THIS FILE. An audit of the previous round measured
// that the deletion of delivery-gate-armed.yml is reported by nothing: it does
// not go red, it stops existing, and an absent check reads on a pull request
// exactly like a repository that never had one.
func TestObserversCheck_FailsWhenTheArmedWorkflowIsDeleted(t *testing.T) {
	root := observerTree(t)
	if err := os.Remove(filepath.Join(root, ".github/workflows/delivery-gate-armed.yml")); err != nil {
		t.Fatal(err)
	}

	code, out := runObserversCheck(t, root)
	if code == 0 {
		t.Fatalf("the only check that reports an unarmed delivery gate was deleted and this reported everything intact:\n%s", out)
	}
	if !strings.Contains(out, "OBSERVER 1") || !strings.Contains(out, "IS NOT THERE") {
		t.Errorf("the refusal must name the file that is gone; got:\n%s", out)
	}
}

// The same absence, softer and much harder to see: the file survives every
// listing and never fires for the commit shape that disarmed the gate all three
// times — a change under `.claude/**` and nowhere else.
func TestObserversCheck_FailsWhenTheArmedWorkflowGrowsAPathsFilter(t *testing.T) {
	root := observerTree(t)
	rewriteObserver(t, root, ".github/workflows/delivery-gate-armed.yml",
		"  push:\n    branches: [workspace, develop, main]",
		"  push:\n    branches: [workspace, develop, main]\n    paths: ['**/*.go']")

	code, out := runObserversCheck(t, root)
	if code == 0 {
		t.Fatalf("the armed-check was filtered to paths that no disarming commit touches, and this called it intact:\n%s", out)
	}
	if !strings.Contains(out, "paths") {
		t.Errorf("the refusal must name the filter; got:\n%s", out)
	}
}

// `workspace` is the point of the branch list, not padding in it: the measured
// window was 21 commits on `workspace`, and dropping it puts the earliest
// possible report back on the promotion PR — the four-day delay under a new
// name.
func TestObserversCheck_FailsWhenTheArmedWorkflowStopsWatchingWorkspace(t *testing.T) {
	root := observerTree(t)
	rewriteObserver(t, root, ".github/workflows/delivery-gate-armed.yml",
		"branches: [workspace, develop, main]", "branches: [develop, main]")

	code, out := runObserversCheck(t, root)
	if code == 0 {
		t.Fatalf("the armed-check stopped running on the branch the whole window happened on, and this called it intact:\n%s", out)
	}
	if !strings.Contains(out, "workspace") {
		t.Errorf("the refusal must name the branch that is no longer covered; got:\n%s", out)
	}
}

// A trailing comment defeated a widening in this repository once already, so the
// branch list has to read the same with one attached.
func TestObserversCheck_ReadsABranchListThatCarriesATrailingComment(t *testing.T) {
	root := observerTree(t)
	rewriteObserver(t, root, ".github/workflows/delivery-gate-armed.yml",
		"branches: [workspace, develop, main]",
		"branches: [workspace, develop, main]  # workspace is the point of the list")

	code, out := runObserversCheck(t, root)
	if code != 0 {
		t.Fatalf("a comment after the branch list made an intact observer read as broken:\nexit %d\n%s", code, out)
	}
}

// The block spelling of the same list is the one `gh` and most editors produce
// on a reformat. Reading only the flow spelling would turn a formatting change
// into a red gate, and a gate that is red for formatting gets switched off.
func TestObserversCheck_ReadsTheBlockSpellingOfTheBranchList(t *testing.T) {
	root := observerTree(t)
	rewriteObserver(t, root, ".github/workflows/delivery-gate-armed.yml",
		"    branches: [workspace, develop, main]",
		"    branches:\n      - workspace\n      - develop\n      - main")

	code, out := runObserversCheck(t, root)
	if code != 0 {
		t.Fatalf("the block spelling of an intact branch list read as broken:\nexit %d\n%s", code, out)
	}
}

// Present, correctly triggered, and checking nothing.
func TestObserversCheck_FailsWhenTheArmedWorkflowNoLongerRunsItsScript(t *testing.T) {
	root := observerTree(t)
	rewriteObserver(t, root, ".github/workflows/delivery-gate-armed.yml",
		"      - run: python3 .github/scripts/assert_delivery_gate_armed.py",
		"      - run: echo ok")

	code, out := runObserversCheck(t, root)
	if code == 0 {
		t.Fatalf("the armed-check workflow runs nothing and this called it intact:\n%s", out)
	}
	if !strings.Contains(out, "assert_delivery_gate_armed.py") {
		t.Errorf("the refusal must name the check that is no longer invoked; got:\n%s", out)
	}
}

// The multi-line spelling of the same step. Anyone adding a second command to a
// `run:` reaches for `run: |`, and a check that reads only the single-line form
// would turn that reformat into a red gate — which is exactly how a gate stops
// being trusted and then stops being on.
func TestObserversCheck_ReadsAnInvocationInsideARunBlock(t *testing.T) {
	root := observerTree(t)
	rewriteObserver(t, root, ".github/workflows/workflow-hygiene.yml",
		"        run: python3 .github/scripts/assert_gate_observers_intact.py",
		"        run: |\n          python3 .github/scripts/assert_gate_observers_intact.py")

	code, out := runObserversCheck(t, root)
	if code != 0 {
		t.Fatalf("an invocation inside a `run: |` block read as no invocation at all:\nexit %d\n%s", code, out)
	}
}

func TestObserversCheck_FailsWhenTheArmedScriptIsDeleted(t *testing.T) {
	root := observerTree(t)
	if err := os.Remove(filepath.Join(root, ".github/scripts/assert_delivery_gate_armed.py")); err != nil {
		t.Fatal(err)
	}

	code, out := runObserversCheck(t, root)
	if code == 0 {
		t.Fatalf("the armed-check's script is gone and this called the mechanism intact:\n%s", out)
	}
	if !strings.Contains(out, "assert_delivery_gate_armed.py") {
		t.Errorf("the refusal must name the missing script; got:\n%s", out)
	}
}

// The second observer is the one that reports the first one's deletion FROM
// GITHUB. Losing it does not stop this local check from running, and this local
// check is exactly where its loss has to surface.
func TestObserversCheck_FailsWhenTheHygieneWorkflowIsDeleted(t *testing.T) {
	root := observerTree(t)
	if err := os.Remove(filepath.Join(root, ".github/workflows/workflow-hygiene.yml")); err != nil {
		t.Fatal(err)
	}

	code, out := runObserversCheck(t, root)
	if code == 0 {
		t.Fatalf("the workflow that reports a deleted armed-check is itself gone and this reported everything intact:\n%s", out)
	}
	if !strings.Contains(out, "OBSERVER 2") {
		t.Errorf("the refusal must name the second observer; got:\n%s", out)
	}
}

// A `paths:` filter is allowed on the hygiene workflow — it runs actionlint in
// docker and is the heavier of the two — but a filter that does not match
// `.github/` would not start it for the deletion it exists to report.
func TestObserversCheck_FailsWhenTheHygieneFilterStopsCoveringDotGithub(t *testing.T) {
	root := observerTree(t)
	rewriteObserver(t, root, ".github/workflows/workflow-hygiene.yml",
		"    paths: [\".github/**\"]", "    paths: [\"docs/**\"]")

	code, out := runObserversCheck(t, root)
	if code == 0 {
		t.Fatalf("the reporting workflow no longer starts for a change under .github/, and this called it intact:\n%s", out)
	}
	if !strings.Contains(out, ".github/") {
		t.Errorf("the refusal must name the path it no longer covers; got:\n%s", out)
	}
}

// Fail closed. A workflow whose triggers cannot be read is not a workflow
// anybody can claim fires, and "I could not look" must never print as "I looked
// and it was fine".
//
// THE FIXTURE IS BUILT SO THAT NOTHING ELSE CAN FAIL IT, and that is the point
// of it. A first version of this test used `jobs: {}`, which ALSO drops the
// `run:` step — so it went red on the missing invocation and stayed green when a
// mutant folded NOT CHECKED into the pass. The property was never pinned; the
// test only looked like it was. Here the file is present, invokes the armed
// check, and is unreadable in exactly one respect: it has no `on:` block. The
// only verdict that can fail this run is the NOT CHECKED one.
func TestObserversCheck_FailsClosedOnAWorkflowWhoseTriggersItCannotRead(t *testing.T) {
	root := observerTree(t)
	if err := os.WriteFile(filepath.Join(root, ".github/workflows/delivery-gate-armed.yml"),
		[]byte("name: delivery-gate-armed\njobs:\n  armed:\n    steps:\n"+
			"      - run: python3 .github/scripts/assert_delivery_gate_armed.py\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out := runObserversCheck(t, root)
	if code == 0 {
		t.Fatalf("a workflow with no readable `on:` block was reported as correctly triggered:\n%s", out)
	}
	if !strings.Contains(out, "NOT CHECKED") {
		t.Errorf("an unreadable trigger block must be reported as NOT CHECKED, never folded into a pass; got:\n%s", out)
	}
	// The guard that makes the assertion above mean something: if any OTHER
	// verdict is red here, this test would pass with the fail-closed rule
	// removed, which is how it passed for the wrong reason once already.
	if strings.Count(out, "::error::") != 1 {
		t.Errorf("exactly one verdict may be red in this fixture, or this test does not pin fail-closed; got:\n%s", out)
	}
}
