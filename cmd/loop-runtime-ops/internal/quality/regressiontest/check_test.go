// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package regressiontest

import (
	"testing"
)

// TestCheck_NoBugRef_Skip — no issue references → gate skips (PASS).
func TestCheck_NoBugRef_Skip(t *testing.T) {
	o := Check(Config{Title: "feat: add new endpoint", Body: "adds a thing"})
	if !o.Pass {
		t.Errorf("expected PASS (skip — no bug ref), got %v", o.Reason)
	}
	if !o.Skipped {
		t.Error("expected Skipped=true")
	}
}

// TestCheck_BugRefWithTest_Pass — bug ref + new test in diff → PASS.
func TestCheck_BugRefWithTest_Pass(t *testing.T) {
	o := Check(Config{
		Title:     "fix: resolve panic (fixes #123)",
		DiffFiles: "api/foo.go\napi/foo_test.go\n",
	})
	if !o.Pass {
		t.Errorf("expected PASS (bug ref + test), got %v", o.Reason)
	}
	if len(o.IssueRefs) == 0 {
		t.Error("expected issue refs detected")
	}
}

// TestCheck_BugRefNoTest_Fail — bug ref but no test in diff → FAIL.
func TestCheck_BugRefNoTest_Fail(t *testing.T) {
	o := Check(Config{
		Title:     "Bug #42: fix the thing",
		DiffFiles: "api/foo.go\nREADME.md\n",
	})
	if o.Pass {
		t.Error("expected FAIL (bug ref, no test)")
	}
}

// TestCheck_RefInBody — issue ref detected in body.
func TestCheck_RefInBody(t *testing.T) {
	o := Check(Config{
		Body:      "This closes #99 by adding bounds check.",
		DiffFiles: "pkg/x_test.go\n",
	})
	if !o.Pass || len(o.IssueRefs) == 0 {
		t.Errorf("expected PASS with refs from body, got pass=%v refs=%v", o.Pass, o.IssueRefs)
	}
}

// TestCheck_RefInCommits — issue ref detected in commit messages.
func TestCheck_RefInCommits(t *testing.T) {
	o := Check(Config{
		Commits:   "wip\nfix #7 handle nil\n",
		DiffFiles: "cli/a.test.ts\n",
	})
	if !o.Pass || len(o.IssueRefs) == 0 {
		t.Errorf("expected PASS with refs from commits, got pass=%v refs=%v", o.Pass, o.IssueRefs)
	}
}

// TestExtractIssueRefs_Keywords — all keyword variants + dedup.
func TestExtractIssueRefs_Keywords(t *testing.T) {
	text := "fix #1 fixes #2 fixed #3 close #4 closes #5 closed #6 resolve #7 resolves #8 resolved #9 Bug #10 fix #1"
	refs := extractIssueRefs(text)
	// #1 appears twice → dedup; expect 10 unique
	if len(refs) != 10 {
		t.Errorf("expected 10 unique refs, got %d: %v", len(refs), refs)
	}
}

// TestExtractIssueRefs_CaseInsensitive — keywords match case-insensitively.
func TestExtractIssueRefs_CaseInsensitive(t *testing.T) {
	refs := extractIssueRefs("FIXES #11 and CLOSES #12")
	if len(refs) != 2 {
		t.Errorf("expected 2 refs (case-insensitive), got %v", refs)
	}
}

// TestExtractIssueRefs_NoFalsePositive — bare #N without keyword not matched.
func TestExtractIssueRefs_NoFalsePositive(t *testing.T) {
	refs := extractIssueRefs("see PR #50 for context, mentions item #51")
	if len(refs) != 0 {
		t.Errorf("expected 0 refs (no fix/close keyword), got %v", refs)
	}
}

// TestIsTestFile_AllExtensions — extension classifier.
func TestIsTestFile_AllExtensions(t *testing.T) {
	yes := []string{"a_test.go", "b.test.ts", "c.spec.ts", "d.test.tsx", "e.spec.tsx"}
	no := []string{"a.go", "b.ts", "README.md"}
	for _, f := range yes {
		if !isTestFile(f) {
			t.Errorf("isTestFile(%q) should be true", f)
		}
	}
	for _, f := range no {
		if isTestFile(f) {
			t.Errorf("isTestFile(%q) should be false", f)
		}
	}
}
