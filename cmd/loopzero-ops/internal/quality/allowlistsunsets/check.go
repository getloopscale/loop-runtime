// Package allowlistsunsets is a hermetic contract test for
// .claude/scripts/check-allowlist-sunsets.py (T5.2.a of plan
// 2026-06-11-code-review-remediation-147-findings v1.2).
//
// Seeds fixture allowlists (future / today / past / malformed / bad-date)
// and asserts the Python gate's exit codes:
//
//	0 — all sunsets future OR today (today is WARN, not block)
//	1 — at least one sunset is past
//	2 — parse error (malformed entry OR bad date format)
//
// Also runs a LIVE check against the real rules/code-review-allowlist.txt.
//
// Migrated from infra/tests/check-allowlist-sunsets_test.sh
// (M-Migrate2 continuation 2026-07-07). Requires python3 on PATH.
package allowlistsunsets

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	gatePath          = ".claude/scripts/check-allowlist-sunsets.py"
	liveAllowlistPath = "rules/code-review-allowlist.txt"
)

// Config controls the check.
type Config struct {
	RepoRoot string
	// Today allows tests to inject a fixed reference date; when zero, time.Now() is used.
	Today time.Time
}

// Outcome is the summary.
type Outcome struct {
	Failures []string
}

// ErrViolations means at least one fixture assertion failed.
var ErrViolations = errors.New("check-allowlist-sunsets fixture contract violated")

// Check runs the seven assertions from the bash guard.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	gate := filepath.Join(root, gatePath)

	if _, err := os.Stat(gate); err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("gate script missing at %s: %v", gatePath, err)}}, ErrViolations
	}
	if _, err := exec.LookPath("python3"); err != nil {
		return Outcome{Failures: []string{fmt.Sprintf("python3 not on PATH: %v (this gate is a driver for the Python script and requires python3)", err)}}, ErrViolations
	}

	today := cfg.Today
	if today.IsZero() {
		today = time.Now().UTC()
	}
	futureDate := today.Add(30 * 24 * time.Hour).Format("2006-01-02")
	todayStr := today.Format("2006-01-02")
	pastDate := today.Add(-24 * time.Hour).Format("2006-01-02")

	tmpdir, err := os.MkdirTemp("", "allowlist-sunsets-")
	if err != nil {
		return Outcome{}, fmt.Errorf("mkdirtemp: %w", err)
	}
	defer os.RemoveAll(tmpdir)

	var failures []string
	runFixture := func(name, body string, wantRC int, mustContain string) {
		p := filepath.Join(tmpdir, name+".txt")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			failures = append(failures, fmt.Sprintf("%s: write fixture failed: %v", name, err))
			return
		}
		cmd := exec.Command("python3", gate, p)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		runErr := cmd.Run()
		gotRC := 0
		if runErr != nil {
			if ee, ok := runErr.(*exec.ExitError); ok {
				gotRC = ee.ExitCode()
			} else {
				failures = append(failures, fmt.Sprintf("%s: unexpected run error: %v", name, runErr))
				return
			}
		}
		if gotRC != wantRC {
			failures = append(failures, fmt.Sprintf("%s: expected exit %d, got %d (stdout=%q stderr=%q)", name, wantRC, gotRC, stdout.String(), stderr.String()))
			return
		}
		merged := stdout.String() + stderr.String()
		if mustContain != "" && !strings.Contains(merged, mustContain) {
			failures = append(failures, fmt.Sprintf("%s: output missing %q marker (got %q)", name, mustContain, merged))
		}
	}

	// Fixture 1: all future entries → exit 0 + PASS marker
	runFixture("future", fmt.Sprintf("# header comment\nfid-1 | refactor | %s | test rationale\nfid-2 | maturity | %s | another rationale\n", futureDate, futureDate),
		0, "PASS")
	// Fixture 2: today-only → exit 0 + WARN
	runFixture("today", fmt.Sprintf("fid-today | refactor | %s | test today entry\n", todayStr),
		0, "WARN")
	// Fixture 3: past → exit 1 + 'past sunset'
	runFixture("past", fmt.Sprintf("fid-past | refactor | %s | test past entry\n", pastDate),
		1, "past sunset")
	// Fixture 4: malformed (only 2 columns) → exit 2 + 'malformed'
	runFixture("malformed", "fid-bad | only_two_columns\n",
		2, "malformed")
	// Fixture 5: bad date format → exit 2 + 'bad sunset_date'
	runFixture("baddate", "fid-baddate | refactor | not-a-date | wrong format\n",
		2, "bad sunset_date")
	// Fixture 6: missing file → exit 0 (no deferrals declared)
	{
		missing := filepath.Join(tmpdir, "does-not-exist.txt")
		cmd := exec.Command("python3", gate, missing)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		runErr := cmd.Run()
		gotRC := 0
		if runErr != nil {
			if ee, ok := runErr.(*exec.ExitError); ok {
				gotRC = ee.ExitCode()
			} else {
				failures = append(failures, fmt.Sprintf("missing: unexpected run error: %v", runErr))
			}
		}
		if gotRC != 0 {
			failures = append(failures, fmt.Sprintf("missing: expected exit 0, got %d (stdout=%q stderr=%q)", gotRC, stdout.String(), stderr.String()))
		}
	}
	// LIVE: real rules/code-review-allowlist.txt → exit 0
	livePath := filepath.Join(root, liveAllowlistPath)
	if _, err := os.Stat(livePath); err == nil {
		cmd := exec.Command("python3", gate, livePath)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		runErr := cmd.Run()
		gotRC := 0
		if runErr != nil {
			if ee, ok := runErr.(*exec.ExitError); ok {
				gotRC = ee.ExitCode()
			}
		}
		if gotRC != 0 {
			failures = append(failures, fmt.Sprintf("live: real %s has past-sunset entries (exit=%d output=%q)", liveAllowlistPath, gotRC, stdout.String()+stderr.String()))
		}
	}
	// If the file does not exist, treat as no deferrals declared (parity with fixture 6).

	if len(failures) > 0 {
		return Outcome{Failures: failures}, ErrViolations
	}
	return Outcome{}, nil
}
