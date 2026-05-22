// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package actionpinning

import (
	"os"
	"path/filepath"
	"testing"
)

func writeWorkflow(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// TestCheck_ShaPinned_Pass — a 40-char SHA ref is compliant.
func TestCheck_ShaPinned_Pass(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "ci.yml",
		"jobs:\n  build:\n    steps:\n      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683 # v4.2.2\n")
	o, err := Check(Config{WorkflowsDir: dir})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass, got violations %v", o.Violations)
	}
	if o.WorkflowCount != 1 {
		t.Errorf("expected 1 workflow, got %d", o.WorkflowCount)
	}
}

// TestCheck_TagPinned_Fail — a @v<x> ref is a violation.
func TestCheck_TagPinned_Fail(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "ci.yml", "      - uses: actions/checkout@v4\n")
	o, err := Check(Config{WorkflowsDir: dir})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail for @v4 tag pin")
	}
	if len(o.Violations) != 1 {
		t.Errorf("expected 1 violation, got %v", o.Violations)
	}
}

// TestCheck_BranchRef_Fail — a branch ref (e.g. @main) is a violation.
func TestCheck_BranchRef_Fail(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "ci.yml", "      - uses: some/action@main\n")
	o, err := Check(Config{WorkflowsDir: dir})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail for @main branch ref")
	}
}

// TestCheck_ShortHash_Fail — a 7-char short hash is NOT 40 hex chars.
func TestCheck_ShortHash_Fail(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "ci.yml", "      - uses: some/action@abc1234\n")
	o, err := Check(Config{WorkflowsDir: dir})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail for short hash")
	}
}

// TestCheck_InternalReusable_Exempt — relative-path workflows are exempt.
func TestCheck_InternalReusable_Exempt(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "ci.yml",
		"      - uses: ./.github/workflows/reusable-build.yml\n      - uses: .github/workflows/x.yml\n")
	o, err := Check(Config{WorkflowsDir: dir})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass for internal reusable, got %v", o.Violations)
	}
}

// TestCheck_PragmaExempt — # pragma: allow-tag-pin exempts the next uses line.
func TestCheck_PragmaExempt(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "ci.yml",
		"      # pragma: allow-tag-pin\n      - uses: actions/checkout@v4\n")
	o, err := Check(Config{WorkflowsDir: dir})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass with pragma, got %v", o.Violations)
	}
}

// TestCheck_PragmaWithCommentBetween — pragma survives a comment line in window.
func TestCheck_PragmaWithCommentBetween(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "ci.yml",
		"      # pragma: allow-tag-pin\n      # TODO(INF-G18): Renovate resolves to @<sha40> on first run.\n      - uses: actions/checkout@v4\n")
	o, err := Check(Config{WorkflowsDir: dir})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass with pragma+comment, got %v", o.Violations)
	}
}

// TestCheck_PragmaBrokenByCode — a non-comment line breaks pragma association.
func TestCheck_PragmaBrokenByCode(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "ci.yml",
		"      # pragma: allow-tag-pin\n      - name: a real step\n      - uses: actions/checkout@v4\n")
	o, err := Check(Config{WorkflowsDir: dir})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail — pragma association broken by code line")
	}
}

// TestCheck_PragmaTooFarAway — pragma beyond PragmaLookback window does not apply.
func TestCheck_PragmaTooFarAway(t *testing.T) {
	dir := t.TempDir()
	// 4 comment lines between pragma and uses pushes pragma out of the
	// PragmaLookback=4 window (window holds the 4 lines just above uses).
	writeWorkflow(t, dir, "ci.yml",
		"      # pragma: allow-tag-pin\n      # c1\n      # c2\n      # c3\n      # c4\n      - uses: actions/checkout@v4\n")
	o, err := Check(Config{WorkflowsDir: dir})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail — pragma outside lookback window")
	}
}

// TestCheck_NoUsesLines_Pass — a workflow with no uses: is trivially compliant.
func TestCheck_NoUsesLines_Pass(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "ci.yml", "name: CI\non: push\njobs:\n  noop:\n    runs-on: ubuntu-latest\n")
	o, err := Check(Config{WorkflowsDir: dir})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass, got %v", o.Violations)
	}
}

// TestCheck_MissingDir_Error — nonexistent dir returns error.
func TestCheck_MissingDir_Error(t *testing.T) {
	if _, err := Check(Config{WorkflowsDir: "/nonexistent/workflows"}); err == nil {
		t.Error("expected error for missing dir")
	}
}

// TestCheck_MultipleFiles_Sorted — violations across files are collected.
func TestCheck_MultipleFiles_Sorted(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir, "a.yml", "      - uses: actions/checkout@v4\n")
	writeWorkflow(t, dir, "b.yml", "      - uses: actions/setup-go@v5\n")
	o, err := Check(Config{WorkflowsDir: dir})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass || len(o.Violations) != 2 {
		t.Errorf("expected 2 violations, got %v", o.Violations)
	}
	if o.WorkflowCount != 2 {
		t.Errorf("expected 2 workflows, got %d", o.WorkflowCount)
	}
}

// TestIsCompliant — unit coverage of the ref classifier.
func TestIsCompliant(t *testing.T) {
	cases := map[string]bool{
		"actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683": true,
		"actions/checkout@v4":                  false,
		"actions/checkout@main":                false,
		"./.github/workflows/reusable.yml":     true,
		".github/workflows/x.yml":              true,
		"docker/build-push-action@abc":         false,
		"local-no-at-ref":                      true, // defensive: no @
	}
	for ref, want := range cases {
		if got := isCompliant(ref); got != want {
			t.Errorf("isCompliant(%q) = %v, want %v", ref, got, want)
		}
	}
}
