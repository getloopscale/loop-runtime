// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package runbookparity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/qtestutil"
)

var repoRoot = qtestutil.MustRepoRoot()

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const runbookOK = "The correct SA is `loop-runtime-workflow-submitter` (bound to a Role).\n" +
	"Do **NOT** mint `loop-runtime-build-sa` here — it is the executor.\n" +
	"```bash\nkubectl -n argo create token loop-runtime-workflow-submitter --duration=720h\n```\n"

// THE DEFECT MEASURED on 2026-08-18: the error message said to rotate
// `loop-runtime-build-sa`, and the runbook explicitly says NOT to do that — the wrong SA
// authenticates and is then forbidden, trading a readable 401 for a 403 that looks like a
// fresh failure.
func TestMessageContradictingTheRunbookFails(t *testing.T) {
	root := tree(t, map[string]string{
		runbookRel: runbookOK,
		"api/internal/services/build/argo_step_watcher.go": "logger.Error(\"argo step watcher: build cluster auth failed — rotate loop-runtime-build-sa token\",\n",
	})
	out, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrContradiction) {
		t.Fatalf("a message that says to do what the runbook forbids fails; err=%v", err)
	}
	if len(out.Findings) != 1 || out.Findings[0].Named != "loop-runtime-build-sa" {
		t.Fatalf("the finding must NAME the wrong SA and the right one: %+v", out.Findings)
	}
	if out.Findings[0].Prescribed != "loop-runtime-workflow-submitter" {
		t.Fatalf("the prescribed SA must come from the runbook: %+v", out.Findings[0])
	}
}

func TestMessageAgreeingWithTheRunbookPasses(t *testing.T) {
	root := tree(t, map[string]string{
		runbookRel: runbookOK,
		"api/internal/services/build/argo_step_watcher.go": "logger.Error(\"argo step watcher: build cluster auth failed — rotate loop-runtime-workflow-submitter token\",\n",
	})
	if _, err := violationOf(Check(Config{RepoRoot: root})); err != nil {
		t.Fatalf("an aligned message passes: %v", err)
	}
}

// Absence is a FAILURE: without a runbook there is no prescription to compare against,
// and reporting "no contradiction" would be the usual green-over-nothing.
func TestMissingRunbookFails(t *testing.T) {
	root := tree(t, map[string]string{
		"api/internal/services/build/argo_step_watcher.go": "logger.Error(\"rotate loop-runtime-build-sa token\")\n",
	})
	if _, err := violationOf(Check(Config{RepoRoot: root})); !errors.Is(err, ErrRunbookMissing) {
		t.Fatalf("without a runbook there is nothing to compare; err=%v", err)
	}
}

func TestZeroMessagesScannedFails(t *testing.T) {
	root := tree(t, map[string]string{
		runbookRel: runbookOK,
		"api/x.go": "package x\n",
	})
	if _, err := violationOf(Check(Config{RepoRoot: root})); !errors.Is(err, ErrNothingScanned) {
		t.Fatalf("zero messages scanned fails; err=%v", err)
	}
}

// The real population.
func TestRealTree(t *testing.T) {
	out, err := violationOf(Check(Config{RepoRoot: repoRoot}))
	if err != nil {
		for _, f := range out.Findings {
			t.Errorf("%s:%d — says to rotate %q, runbook prescribes %q", f.File, f.Line, f.Named, f.Prescribed)
		}
		t.Fatalf("real tree: %v", err)
	}
	if out.Scanned == 0 {
		t.Fatal("zero messages scanned — finding nothing is not the same as finding nothing wrong")
	}
}

// The REAL SHAPE of the runbook, and the gate was blind to it.
//
// `Do **NOT** mint` ends the line and the SA name comes on the NEXT one, behind a markdown
// `> ` quote marker. The match stopped at the `>`, so `forbidden` stayed empty and the
// population scan passed — not because the tree was clean, but because the detector could
// not see. Third time this trap shows up in this session (B-090, B-019, this one).
func TestProhibitionSplitAcrossTWOQuotedLines(t *testing.T) {
	real := "> watch** build Workflows is `loop-runtime-workflow-submitter` (bound to a Role with\n" +
		"> `workflows: [create, get, list, watch, delete]`). Do **NOT** mint\n" +
		"> `loop-runtime-build-sa` here — that SA is the workflow *executor*.\n" +
		"```bash\nkubectl -n argo create token loop-runtime-workflow-submitter --duration=720h\n```\n"
	root := tree(t, map[string]string{
		runbookRel: real,
		"api/internal/services/build/argo_step_watcher.go": "logger.Error(\"build cluster auth failed — rotate loop-runtime-build-sa token\",\n",
	})
	out, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrContradiction) {
		t.Fatalf("the prohibition split across two lines must be read; err=%v (%+v)", err, out.Findings)
	}
}

// A COMMENT quoting the old message is documentation, not emission — and the gate matched
// it. It was the very comment recording this fix that triggered the false positive. Same
// trap as `publishhygiene`: the text describing the defect triggers the detector.
// Rewriting the comment would fix it from the outside and leave the gate wrong.
func TestCommentQuotingTheOldMessageIsNotAFinding(t *testing.T) {
	root := tree(t, map[string]string{
		runbookRel: runbookOK,
		"api/internal/services/build/argo_step_watcher.go": "" +
			"\t\t// FIXED: this message used to say \"rotate loop-runtime-build-sa token\", and the runbook forbids it.\n" +
			"\t\tlogger.Error(\"build cluster auth failed — rotate loop-runtime-workflow-submitter token\",\n",
	})
	if _, err := violationOf(Check(Config{RepoRoot: root})); err != nil {
		t.Fatalf("a comment documenting the fix is not a finding: %v", err)
	}
}

// A Go file under api/ the gate cannot read may be the one that contradicts the
// runbook; skipping it reports agreement over code nobody read.
func TestUnreadableGoFileIsAnError(t *testing.T) {
	root := tree(t, map[string]string{
		runbookRel: runbookOK,
		"api/internal/services/build/argo_step_watcher.go": "logger.Error(\"argo step watcher: build cluster auth failed — rotate loop-runtime-workflow-submitter token\",\n",
	})
	if err := os.Symlink(filepath.Join(root, "missing-target"), filepath.Join(root, "api", "unreadable.go")); err != nil {
		t.Fatal(err)
	}

	if _, err := violationOf(Check(Config{RepoRoot: root})); err == nil {
		t.Fatal("an unreadable Go file under api/ must be an error")
	}
}
