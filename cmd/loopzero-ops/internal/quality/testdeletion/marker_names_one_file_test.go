// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testdeletion

import (
	"os"
	"testing"
)

// These tests are RED BY DESIGN: they measure the defect, not its fix.
//
// They SKIP rather than fail because a permanently red test is ignored within a week
// and takes the whole suite with it (`rules/testing.md`: "once red tests are ignored,
// all tests lose their value"). A skip that names the defect and prints the
// measurement is the opposite of the invisible skip that same rule forbids — the debt
// shows up in every run, and the issue below is where it is tracked.
//
// Tracked at: https://github.com/getloopscale/loop-runtime/issues/364
// The defect: the gate reads WHETHER an approval marker exists, not WHICH file it names, so one
// approval authorises deleting any other test in the same commit.
//
// `LOOPZERO_RUN_MARKER_SCOPE=1 go test ./internal/quality/testdeletion/` runs them for real.
// When the issue closes, delete this block — if they pass without the variable, the
// fix has landed.

func skipUnlessMarkerScopeArmed(t *testing.T) {
	t.Helper()
	if os.Getenv("LOOPZERO_RUN_MARKER_SCOPE") == "" {
		// https://github.com/getloopscale/loop-runtime/issues/364
		t.Skip("known debt: the test-deletion gate reads whether an approval marker exists, " +
			"not which file it names — so one approval authorises deleting any other test " +
			"in the same commit. Run with LOOPZERO_RUN_MARKER_SCOPE=1 to see the measurement.")
	}
}

func TestMarkerNamingFileADoesNotApproveFileB(t *testing.T) {
	skipUnlessMarkerScopeArmed(t)
	diff := "api/foo_test.go\napi/unrelated_test.go\n"
	o := mustCheck(t, Config{
		DiffOverride: &diff,
		CommitLog: "chore: drop the foo suite\n\n" +
			"Test deletion justification: api/foo_test.go covered a helper that\n" +
			"was removed in this same slice.\n",
	})
	if o.Pass {
		t.Errorf("a marker naming api/foo_test.go must not approve the deletion of "+
			"api/unrelated_test.go; got Pass with reason %q over deletions %v",
			o.Reason, o.DeletedTests)
	}
}

// B-058 RED — the PR-body channel has the same grain.
func TestPRBodyMarkerNamingFileADoesNotApproveFileB(t *testing.T) {
	skipUnlessMarkerScopeArmed(t)
	diff := "api/foo_test.go\napi/unrelated_test.go\n"
	o := mustCheck(t, Config{
		DiffOverride: &diff,
		PRBody:       "Test deletion justification: api/foo_test.go is superseded.\n",
	})
	if o.Pass {
		t.Errorf("a PR-body marker naming api/foo_test.go must not approve "+
			"api/unrelated_test.go; got Pass with reason %q", o.Reason)
	}
}

// B-058 RED — a marker in a commit that deletes nothing approves every
// deletion made by every other commit in the range. This is the shape
// measured live on 2026-09-12: neither of the two markers in
// origin/main...workspace deletes a test file, and all 12 deletions passed.
func TestMarkerInACommitThatDeletesNothingApprovesNothing(t *testing.T) {
	skipUnlessMarkerScopeArmed(t)
	diff := "api/foo_test.go\n"
	o := mustCheck(t, Config{
		DiffOverride: &diff,
		CommitLog: "refactor: move a helper\n\n(no deletions here)\n" +
			"\x00\x00" + // separator stand-in; %B concatenates bodies
			"chore: justify a deletion from another session\n\n" +
			"Test deletion justification: something else entirely.\n",
	})
	if o.Pass {
		t.Errorf("a marker in a commit that deletes no test file must not approve "+
			"a deletion made elsewhere in the range; got Pass with reason %q", o.Reason)
	}
}
