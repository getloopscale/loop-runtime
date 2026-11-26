// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package allowlistsunsets

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The gate reads the allowlist itself instead of shelling out.
//
// It was a hermetic contract test for `.claude/scripts/check-allowlist-sunsets.py`,
// asserting that script's exit codes against seeded fixtures. Measured 2026-09-16: that
// script exists in no clone, in no installed kit, and in this repository's history
// nowhere reachable — so the gate tested a program that is gone, and reported its absence
// as a failure nobody could act on.
//
// What was NOT gone is the contract it guarded: `code-quality-golden-rule.md` § 4 caps a
// sunset at 90 days and says an expired entry stops exempting anything. With the script
// missing, that contract had no enforcer — and the live allowlist was carrying two
// entries seven days past their own deadline when this was written, which is what an
// unenforced sunset looks like.
//
// Rung 1 of the parsimony ladder disposes of the fixture half: a contract test for a
// program that does not exist does not need to exist. Rung 6 keeps the other half.
func TestPastSunsetIsRefused(t *testing.T) {
	root := seed(t, "ID-1 | refactor | 2026-01-05 | something deferred\n")
	out, err := violationOf(Check(Config{RepoRoot: root, Today: mustDay(t, "2026-09-16")}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("a sunset eight months past must be refused; err=%v failures=%v", err, out.Failures)
	}
	if len(out.Failures) != 1 {
		t.Fatalf("expected exactly one failure, got %v", out.Failures)
	}
}

// Today is a warning, not a wall. The entry is still valid for the whole of its last day,
// and failing on it would refuse work on the one day somebody could still act on it.
func TestSunsetTodayWarnsAndPasses(t *testing.T) {
	root := seed(t, "ID-1 | refactor | 2026-09-16 | expires today\n")
	out, err := violationOf(Check(Config{RepoRoot: root, Today: mustDay(t, "2026-09-16")}))
	if err != nil {
		t.Fatalf("a sunset dated today must pass: %v", err)
	}
	if len(out.Warnings) != 1 {
		t.Fatalf("a sunset dated today must warn; warnings=%v", out.Warnings)
	}
}

func TestFutureSunsetPassesSilently(t *testing.T) {
	root := seed(t, "ID-1 | refactor | 2027-01-01 | plenty of time\n")
	out, err := violationOf(Check(Config{RepoRoot: root, Today: mustDay(t, "2026-09-16")}))
	if err != nil {
		t.Fatalf("a future sunset must pass: %v", err)
	}
	if len(out.Warnings) != 0 || len(out.Failures) != 0 {
		t.Fatalf("a future sunset must be silent; warnings=%v failures=%v", out.Warnings, out.Failures)
	}
}

// A date nobody can parse is not a date in the future. Reading it as one would turn a
// typo into a permanent exemption, which is the failure mode the sunset exists to close.
func TestUnparseableDateIsRefused(t *testing.T) {
	root := seed(t, "ID-1 | refactor | 09/05/2026 | wrong format\n")
	if _, err := violationOf(Check(Config{RepoRoot: root, Today: mustDay(t, "2026-09-16")})); !errors.Is(err, ErrViolations) {
		t.Fatalf("an unparseable sunset must be refused, got err=%v", err)
	}
}

// A line with too few columns carries no sunset at all.
func TestMalformedEntryIsRefused(t *testing.T) {
	root := seed(t, "ID-1 | refactor\n")
	if _, err := violationOf(Check(Config{RepoRoot: root, Today: mustDay(t, "2026-09-16")})); !errors.Is(err, ErrViolations) {
		t.Fatalf("a malformed entry must be refused, got err=%v", err)
	}
}

// Comments and blank lines are not entries.
func TestCommentsAndBlanksAreNotEntries(t *testing.T) {
	root := seed(t, "# a comment | with | pipes | in it\n\n   \nID-1 | refactor | 2027-01-01 | fine\n")
	if _, err := violationOf(Check(Config{RepoRoot: root, Today: mustDay(t, "2026-09-16")})); err != nil {
		t.Fatalf("comments and blanks must be skipped: %v", err)
	}
}

func seed(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "rules"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, liveAllowlistPath), []byte(body), 0o600); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}
	return root
}

func mustDay(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		t.Fatalf("parse %s: %v", s, err)
	}
	return d
}
