// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package liveprobescript holds the hermetic contract every allowlisted live
// probe script under infra/tests/e2e/live/ must satisfy, and one Spec per
// script. Adding a script to the contract is a new Spec row, not a new gate
// package.
//
// Every script is checked for: the file exists, `bash -n` accepts it, it
// declares `set -euo pipefail`, it defines `log()` and `assert()` at line
// start, it references what it probes (the Spec's own patterns), it exits only
// with 0/1/2, and it is listed in infra/quality/shell-scripts-allowlist.md.
// Requires bash on PATH.
package liveprobescript

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qgshell"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// AllowlistRel is the registry every live probe script must be listed in.
const AllowlistRel = "infra/quality/shell-scripts-allowlist.md"

// Pattern is one content assertion: the script must match Re, or Failure is
// reported.
type Pattern struct {
	Re      *regexp.Regexp
	Failure string
}

// commonPatterns hold for every live probe script.
var commonPatterns = []Pattern{
	{regexp.MustCompile(`(?m)^set -euo pipefail`), "set -euo pipefail strict mode: not declared at line start"},
	{regexp.MustCompile(`(?m)^log\(\) \{`), "log function defined: `log() {` not found at line start"},
	{regexp.MustCompile(`(?m)^assert\(\) \{`), "assert function defined: `assert() {` not found at line start"},
}

var (
	exitCodeRE   = regexp.MustCompile(`(?m)^exit ([0-9]+)`)
	allowedExits = map[string]bool{"0": true, "1": true, "2": true}
)

// Spec is one live probe script under contract.
type Spec struct {
	// Gate is the quality gate name that audits this script.
	Gate string
	// Script is the repo-relative path of the script; it is also the tag the
	// allowlist must contain.
	Script string
	// Probes are the script-specific content assertions, checked after the
	// common ones.
	Probes []Pattern
}

// Assertions is the number of assertions Check evaluates for s: existence,
// bash -n, the common patterns, the probes, the exit-code contract and the
// allowlist entry.
func (s Spec) Assertions() int {
	return 2 + len(commonPatterns) + len(s.Probes) + 2
}

// Outcome collects every failed assertion.
type Outcome struct {
	Failures []string
	// Violation is what the check found wrong (wrapping one of its Err*
	// reasons), or nil. A violation is the check's verdict, never its error.
	Violation error
}

// Verdict is Fail when the check found a violation and Pass otherwise.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Violation != nil {
		return verdict.Fail
	}
	return verdict.Pass
}

// ErrViolations is the outcome's Violation when at least one assertion failed;
// the failures are in the Outcome. An error Check returns is structural.
var ErrViolations = errors.New("live probe script contract violated")

// Check evaluates spec against the repository at repoRoot ("" means the
// working directory).
func Check(repoRoot string, spec Spec) (Outcome, error) {
	if repoRoot == "" {
		wd, err := os.Getwd()
		if err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
		repoRoot = wd
	}
	failures := checkScript(filepath.Join(repoRoot, spec.Script), spec)
	failures = append(failures, checkAllowlisted(filepath.Join(repoRoot, AllowlistRel), spec.Script)...)
	if len(failures) > 0 {
		return Outcome{Failures: failures, Violation: ErrViolations}, nil
	}
	return Outcome{}, nil
}

func checkScript(path string, spec Spec) []string {
	data, err := qgshell.StatAndRead(path)
	if err != nil {
		return []string{fmt.Sprintf("file exists: %s missing", spec.Script)}
	}
	var failures []string
	if err := qgshell.RunQuiet("bash", "-n", path); err != nil {
		failures = append(failures, fmt.Sprintf("bash -n %s: %v", spec.Script, err))
	}
	content := string(data)
	for _, p := range append(append([]Pattern{}, commonPatterns...), spec.Probes...) {
		if !p.Re.MatchString(content) {
			failures = append(failures, p.Failure)
		}
	}
	if code, ok := firstForeignExit(content); ok {
		failures = append(failures, fmt.Sprintf(
			"exit 0/1/2 contract: found foreign exit code %s (allowed: 0, 1, 2)", code))
	}
	return failures
}

// firstForeignExit reports the first `exit N` at line start whose code is
// outside the 0/1/2 contract.
func firstForeignExit(content string) (string, bool) {
	for _, m := range exitCodeRE.FindAllStringSubmatch(content, -1) {
		if !allowedExits[m[1]] {
			return m[1], true
		}
	}
	return "", false
}

func checkAllowlisted(allowlistPath, tag string) []string {
	data, err := qgshell.StatAndRead(allowlistPath)
	if err != nil {
		return []string{fmt.Sprintf("allowlist file exists: %s missing", AllowlistRel)}
	}
	if !strings.Contains(string(data), tag) {
		return []string{fmt.Sprintf("script allowlisted: %q not found in %s", tag, AllowlistRel)}
	}
	return nil
}
