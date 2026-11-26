// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testdeletion

import (
	"errors"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/revrange"
)

// TestCheck_NoDeletions_Pass — empty diff → PASS.
func TestCheck_NoDeletions_Pass(t *testing.T) {
	o := mustCheck(t, Config{DiffOverride: ptr("")})
	if !o.Pass {
		t.Errorf("expected Pass on empty diff, got %v", o.Reason)
	}
}

// TestCheck_NonTestDeletion_Pass — deleting a non-test file is not gated.
func TestCheck_NonTestDeletion_Pass(t *testing.T) {
	o := mustCheck(t, Config{DiffOverride: ptr("api/main.go\nREADME.md\n")})
	if !o.Pass {
		t.Errorf("expected Pass (no test files deleted), got %v", o.Reason)
	}
}

// TestCheck_TestDeletionNoApproval_Fail — deleting a test without approval fails.
func TestCheck_TestDeletionNoApproval_Fail(t *testing.T) {
	o := mustCheck(t, Config{DiffOverride: ptr("api/foo_test.go\n")})
	if o.Pass {
		t.Error("expected Fail (test deleted without approval)")
	}
	if len(o.DeletedTests) != 1 || o.DeletedTests[0] != "api/foo_test.go" {
		t.Errorf("expected [api/foo_test.go], got %v", o.DeletedTests)
	}
}

// TestCheck_TestDeletionWithLabel_Pass — approval label suppresses.
func TestCheck_TestDeletionWithLabel_Pass(t *testing.T) {
	o := mustCheck(t, Config{
		DiffOverride: ptr("api/foo_test.go\n"),
		PRLabels:     "some-label, test-deletion-approved , other",
	})
	if !o.Pass {
		t.Errorf("expected Pass (label present), got %v", o.Reason)
	}
}

// TestCheck_TestDeletionWithJustification_Pass — PR body justification suppresses.
func TestCheck_TestDeletionWithJustification_Pass(t *testing.T) {
	o := mustCheck(t, Config{
		DiffOverride: ptr("cli/x.test.ts\n"),
		PRBody:       "This PR removes dead code.\nTest deletion justification: superseded by integration suite.\n",
	})
	if !o.Pass {
		t.Errorf("expected Pass (justification in body), got %v", o.Reason)
	}
}

// TestCheck_JustificationCaseInsensitive — match is case-insensitive.
func TestCheck_JustificationCaseInsensitive(t *testing.T) {
	o := mustCheck(t, Config{
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
		o := mustCheck(t, Config{DiffOverride: ptr(f + "\n")})
		if o.Pass {
			t.Errorf("expected Fail for deleted test %q", f)
		}
	}
}

// TestCheck_MixedDeletions_OnlyTestsGated — non-test deletions ignored, tests gated.
func TestCheck_MixedDeletions_OnlyTestsGated(t *testing.T) {
	o := mustCheck(t, Config{DiffOverride: ptr("api/main.go\napi/foo_test.go\ndocs/x.md\n")})
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

// The justification must be readable from the COMMIT that deletes the test.
//
// PR body and label are both ephemeral and live outside the repository: the
// pre-push hook has neither, so a legitimate deletion cannot be pushed at all,
// and an agent running locally has no channel to justify one. Worse, both are
// editable after the fact — the commit message is the only place where the
// reason travels with the change it explains. Measured 2026-08-06: deleting
// cli-bundle-size (whose subject left the repo with the CLI at M2) blocked
// every push despite the reason being in the commit, the CHANGELOG and the
// wiring ledger.
func TestJustificationInCommitMessage(t *testing.T) {
	diff := "cmd/loopzero-ops/internal/quality/clibundlesize/check_test.go\n"
	o := mustCheck(t, Config{
		DiffOverride: &diff,
		CommitLog: "chore: delete the cli-bundle-size gate\n\n" +
			"Test deletion justification: the gate guarded cli/dist, which left\n" +
			"this repo with the CLI at M2.\n",
	})
	if !o.Pass {
		t.Errorf("a justification in the commit body must satisfy the gate, got %+v", o)
	}
	if !strings.Contains(o.Reason, "commit") {
		t.Errorf("the reason should say where the justification was found, got %q", o.Reason)
	}
}

// Negative case: a deletion whose commit says nothing still fails. The new
// channel must not become a way to pass by having any commit at all.
func TestNoJustificationAnywhereStillFails(t *testing.T) {
	diff := "api/tests/unit/thing_test.go\n"
	o := mustCheck(t, Config{
		DiffOverride: &diff,
		CommitLog:    "chore: tidy up\n\nRemoved some files.\n",
	})
	if o.Pass {
		t.Errorf("a deletion with no justification in any channel must fail, got %+v", o)
	}
}

// A hermetic run must not consult the real repository. DiffOverride means the
// caller is supplying the world; reading `git log` behind its back makes the
// verdict depend on whatever this repo's history happens to say today.
func TestHermeticRunIgnoresRealCommitLog(t *testing.T) {
	diff := "api/tests/unit/thing_test.go\n"
	o := mustCheck(t, Config{DiffOverride: &diff}) // no CommitLog supplied
	if o.Pass {
		t.Error("a hermetic run with no justification must fail regardless of the repo's own history")
	}
}

// A base ref that does not resolve (a shallow clone) must fail the gate: git
// cannot diff, so "no deletions" would be a verdict about a comparison that was
// never made.
func TestCheck_UnreachableBaseRefIsAnError(t *testing.T) {
	_, err := Check(Config{BaseRef: "origin/ref-that-does-not-exist", RepoDir: t.TempDir()})

	if !errors.Is(err, revrange.ErrBaseRefUnreachable) {
		t.Fatalf("err = %v, want revrange.ErrBaseRefUnreachable", err)
	}
}

func mustCheck(t *testing.T, cfg Config) Outcome {
	t.Helper()
	o, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return o
}
