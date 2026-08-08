// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package allowlistsunsets

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCheck_RealRepoPasses uses the actual repo layout — python3 required.
// If python3 is missing on the test host, the test is skipped so this stays
// portable across CI/local dev.
func TestCheck_RealRepoPasses(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH; skipping (this gate is a driver for the Python script)")
	}
	abs, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, statErr := os.Stat(filepath.Join(abs, gatePath)); statErr == nil {
			outcome, checkErr := Check(Config{RepoRoot: abs})
			if checkErr != nil {
				t.Fatalf("real repo must satisfy fixture invariants + live check. err=%v failures=%v", checkErr, outcome.Failures)
			}
			return
		}
		abs = filepath.Dir(abs)
	}
	t.Skip("could not locate the Python gate within 8 parent dirs")
}

// TestCheck_MissingGateFails ensures the check surfaces a helpful failure
// when the Python script is not present in the fixture repo root.
func TestCheck_MissingGateFails(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	root := t.TempDir()
	_, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for missing gate, got %v", err)
	}
}

// The fixtures are generated in Go and graded by the Python gate, so both must
// agree on what "today" is. Go used time.Now().UTC() while
// .claude/scripts/check-allowlist-sunsets.py uses date.today(), which is LOCAL.
// In any negative UTC offset the two disagree for the last hours of the day:
// measured 2026-08-06 21:15 -03, Go generated fixtures dated 2026-08-07 while
// Python graded against 2026-08-06, so the "today" fixture read as future and
// the "past" one as today. The suite failed for three hours every day.
func TestReferenceDateMatchesPythonToday(t *testing.T) {
	// No skip-if-missing here: this whole gate is a driver for a python3 script,
	// and Check already reports a missing python3 as a failure. Skipping would
	// hide the one condition under which the gate cannot work at all.
	out, err := exec.Command("python3", "-c",
		"from datetime import date; print(date.today().isoformat())").Output()
	if err != nil {
		t.Fatalf("python3 is required by this gate and by its test: %v", err)
	}
	pythonToday := strings.TrimSpace(string(out))

	goToday := referenceDate(time.Time{}).Format("2006-01-02")
	if goToday != pythonToday {
		t.Errorf("reference date disagrees with the gate that grades the fixtures:\n"+
			"    Go   = %s\n    Python = %s\n"+
			"    Both must read the same clock, or the fixtures are graded against a different day.",
			goToday, pythonToday)
	}
}
