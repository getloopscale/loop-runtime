// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testdeletion

import "testing"

// B-058 RED — a marker binds to the RANGE, not to the file it names.
//
// The justification below names exactly one file and explains exactly one
// deletion. A second, unrelated test file is deleted in the same range. The
// gate must not approve the second on the strength of a reason written for
// the first.
//
// Current behaviour: Check() reports Pass, because line 97 asks only whether
// the marker appears ANYWHERE in the concatenated commit log.
func TestMarkerNamingFileADoesNotApproveFileB(t *testing.T) {
	diff := "api/foo_test.go\napi/unrelated_test.go\n"
	o := Check(Config{
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
	diff := "api/foo_test.go\napi/unrelated_test.go\n"
	o := Check(Config{
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
	diff := "api/foo_test.go\n"
	o := Check(Config{
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
