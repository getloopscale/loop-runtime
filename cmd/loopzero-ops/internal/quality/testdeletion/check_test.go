// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testdeletion

import (
	"testing"
)

// TestCheck_NoDeletions_Pass — empty diff → PASS.
func TestCheck_NoDeletions_Pass(t *testing.T) {
	o := Check(Config{DiffOverride: ptr("")})
	if !o.Pass {
		t.Errorf("expected Pass on empty diff, got %v", o.Reason)
	}
}

// TestCheck_NonTestDeletion_Pass — deleting a non-test file is not gated.
func TestCheck_NonTestDeletion_Pass(t *testing.T) {
	o := Check(Config{DiffOverride: ptr("api/main.go\nREADME.md\n")})
	if !o.Pass {
		t.Errorf("expected Pass (no test files deleted), got %v", o.Reason)
	}
}

// TestCheck_TestDeletionNoApproval_Fail — deleting a test without approval fails.
func TestCheck_TestDeletionNoApproval_Fail(t *testing.T) {
	o := Check(Config{DiffOverride: ptr("api/foo_test.go\n")})
	if o.Pass {
		t.Error("expected Fail (test deleted without approval)")
	}
	if len(o.DeletedTests) != 1 || o.DeletedTests[0] != "api/foo_test.go" {
		t.Errorf("expected [api/foo_test.go], got %v", o.DeletedTests)
	}
}

// TestCheck_TestDeletionWithLabel_Pass — approval label suppresses.
func TestCheck_TestDeletionWithLabel_Pass(t *testing.T) {
	o := Check(Config{
		DiffOverride: ptr("api/foo_test.go\n"),
		PRLabels:     "some-label, test-deletion-approved , other",
	})
	if !o.Pass {
		t.Errorf("expected Pass (label present), got %v", o.Reason)
	}
}

// TestCheck_TestDeletionWithJustification_Pass — PR body justification suppresses.
func TestCheck_TestDeletionWithJustification_Pass(t *testing.T) {
	o := Check(Config{
		DiffOverride: ptr("cli/x.test.ts\n"),
		PRBody:       "This PR removes dead code.\nTest deletion justification: superseded by integration suite.\n",
	})
	if !o.Pass {
		t.Errorf("expected Pass (justification in body), got %v", o.Reason)
	}
}

// TestCheck_JustificationCaseInsensitive — match is case-insensitive.
func TestCheck_JustificationCaseInsensitive(t *testing.T) {
	o := Check(Config{
		DiffOverride: ptr("x.spec.tsx\n"),
		PRBody:       "TEST DELETION JUSTIFICATION: framework migration",
	})
	if !o.Pass {
		t.Errorf("expected Pass (case-insensitive justification), got %v", o.Reason)
	}
}

// TestCheck_AllTestExtensions — every recognized test extension is gated.
func TestCheck_AllTestExtensions(t *testing.T) {
	exts := []string{"a_test.go", "b.test.ts", "c.spec.ts", "d.test.tsx", "e.spec.tsx"}
	for _, f := range exts {
		o := Check(Config{DiffOverride: ptr(f + "\n")})
		if o.Pass {
			t.Errorf("expected Fail for deleted test %q", f)
		}
	}
}

// TestCheck_MixedDeletions_OnlyTestsGated — non-test deletions ignored, tests gated.
func TestCheck_MixedDeletions_OnlyTestsGated(t *testing.T) {
	o := Check(Config{DiffOverride: ptr("api/main.go\napi/foo_test.go\ndocs/x.md\n")})
	if o.Pass {
		t.Error("expected Fail (one test deleted)")
	}
	if len(o.DeletedTests) != 1 {
		t.Errorf("expected exactly 1 deleted test, got %v", o.DeletedTests)
	}
}

// TestIsTestFile — unit of the extension classifier.
func TestIsTestFile(t *testing.T) {
	cases := map[string]bool{
		"foo_test.go":  true,
		"foo.test.ts":  true,
		"foo.spec.ts":  true,
		"foo.test.tsx": true,
		"foo.spec.tsx": true,
		"foo.go":       false,
		"foo.ts":       false,
		"README.md":    false,
		"test_data.go": false, // not a _test.go
	}
	for f, want := range cases {
		if got := isTestFile(f); got != want {
			t.Errorf("isTestFile(%q) = %v, want %v", f, got, want)
		}
	}
}

func ptr(s string) *string { return &s }
