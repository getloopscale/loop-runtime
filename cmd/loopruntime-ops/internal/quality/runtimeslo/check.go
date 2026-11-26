// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package runtimeslo implements the runtime-SLO gate (ADR D5): per-test ≤60s,
// per-suite ≤600s. Parses `go test -v` output.
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3:
// replaces infra/scripts/quality/check-runtime-slo.sh. Pure-Go (regexp +
// strconv float compare) — replaces bash sed/awk. No external CLI.
package runtimeslo

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

// Limits are the SLO thresholds in seconds.
type Limits struct {
	PerTestSec  float64
	PerSuiteSec float64
}

// DefaultLimits per ADR D5.
func DefaultLimits() Limits { return Limits{PerTestSec: 60, PerSuiteSec: 600} }

// Config drives the gate.
type Config struct {
	LogFile string
	Limits  Limits
}

// Outcome captures the result.
type Outcome struct {
	Pass       bool
	Skipped    bool
	Violations []string
}

// Verdict is Skip when the subject is absent from this checkout, and
// otherwise Pass when the check passed and Fail when it did not.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Skipped {
		return verdict.Skip
	}
	return verdict.FromPass(o.Pass)
}

var (
	// `--- PASS: TestX (60.01s)` (also FAIL/SKIP).
	testLineRe = regexp.MustCompile(`---\s+(?:PASS|FAIL|SKIP):\s+([A-Za-z0-9_/-]+)\s+\(([0-9.]+)s\)`)
	// `ok  pkg/path  10.234s` or `FAIL  pkg/path  10.234s`.
	suiteLineRe = regexp.MustCompile(`^(?:ok|FAIL)\s+(\S+)\s+([0-9.]+)s\b`)
)

// Check runs the gate against a `go test -v` output log.
func Check(cfg Config) (*Outcome, error) {
	f, err := os.Open(cfg.LogFile)
	if err != nil {
		if os.IsNotExist(err) {
			// No log is no verdict: Skipped, and not a pass.
			return &Outcome{Skipped: true}, nil
		}
		return nil, fmt.Errorf("open log %s: %w", cfg.LogFile, err)
	}
	defer f.Close()

	outcome := &Outcome{Pass: true}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if name, dur, ok := parseTestLine(line); ok {
			if dur > cfg.Limits.PerTestSec {
				outcome.Pass = false
				outcome.Violations = append(outcome.Violations,
					fmt.Sprintf("%s: %.2fs > %.0fs (per-test)", name, dur, cfg.Limits.PerTestSec))
			}
			continue
		}
		if suite, dur, ok := parseSuiteLine(line); ok {
			if dur > cfg.Limits.PerSuiteSec {
				outcome.Pass = false
				outcome.Violations = append(outcome.Violations,
					fmt.Sprintf("suite %s: %.2fs > %.0fs (per-suite)", suite, dur, cfg.Limits.PerSuiteSec))
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan log: %w", err)
	}
	return outcome, nil
}

// parseTestLine extracts (name, durationSec, ok) from a `--- PASS/FAIL/SKIP` line.
func parseTestLine(line string) (string, float64, bool) {
	m := testLineRe.FindStringSubmatch(line)
	if m == nil {
		return "", 0, false
	}
	dur, err := strconv.ParseFloat(m[2], 64)
	if err != nil {
		return "", 0, false
	}
	return m[1], dur, true
}

// parseSuiteLine extracts (suite, durationSec, ok) from an `ok|FAIL pkg dur` line.
func parseSuiteLine(line string) (string, float64, bool) {
	m := suiteLineRe.FindStringSubmatch(line)
	if m == nil {
		return "", 0, false
	}
	dur, err := strconv.ParseFloat(m[2], 64)
	if err != nil {
		return "", 0, false
	}
	return m[1], dur, true
}
