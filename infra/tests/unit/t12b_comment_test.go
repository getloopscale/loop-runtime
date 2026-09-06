package unit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A COMMENT SATISFIED THE GUARD THAT EXISTS TO NOTICE A LOST CALLER.
//
// T-12b asserts Taskfile.yml still invokes the canonical bootstrap script, because
// the legacy call was deliberately removed and the canonical one is what covers it.
// It asked `strings.Contains(root, "bootstrap-cell-cc.sh --cell")` — and the Taskfile
// carries that string in a COMMENT.
//
// Measured 2026-09-06 by an audit: deleting BOTH live invocations left the test
// green. The exact condition its own error message is written for, and the guard had
// nothing to say.

func taskfileWithoutComments(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(), "Taskfile.yml"))
	if err != nil {
		t.Fatalf("read Taskfile.yml: %v", err)
	}
	return stripYAMLComments(string(b))
}

func TestT12b_TheInvocationIsFoundInCodeAndNotInAComment(t *testing.T) {
	live := taskfileWithoutComments(t)
	if !strings.Contains(live, "bootstrap-cell-cc.sh --cell") {
		t.Fatal("no LIVE invocation of the canonical script — a comment mentioning it " +
			"is not a caller")
	}
}

func TestT12b_AFileWhereOnlyACommentMentionsItDoesNotSatisfyTheGuard(t *testing.T) {
	// The oracle, as a unit rather than by editing the real Taskfile.
	onlyComment := "tasks:\n  x:\n    # Plan v1.2 T4.2: consolidated bootstrap-cell-cc.sh --cell --env\n    cmds:\n      - echo hi\n"
	if strings.Contains(stripYAMLComments(onlyComment), "bootstrap-cell-cc.sh --cell") {
		t.Fatal("a whole-line comment still satisfies the search")
	}

	live := "tasks:\n  x:\n    cmds:\n      - bash infra/helmfile/scripts/bootstrap-cell-cc.sh --cell 0 --env standalone-multi\n"
	if !strings.Contains(stripYAMLComments(live), "bootstrap-cell-cc.sh --cell") {
		t.Fatal("a live invocation was stripped away")
	}

	trailing := "tasks:\n  x:\n    cmds:\n      - bash scripts/bootstrap-cell-cc.sh --cell 0  # keep\n"
	if !strings.Contains(stripYAMLComments(trailing), "bootstrap-cell-cc.sh --cell") {
		t.Fatal("a live invocation carrying a trailing comment was stripped away")
	}
}
