// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package oidccredadminloopzero enforces the canonical dev credential — admin@loopzero.example
// / loopzero-dev-password (LIVE-validated 2026-06-16; the sponsor, translated: "we will ALWAYS use it
// in tests"). Rejects the fabricated paulo@loopzero.test / LoopZeroDev2026! which does
// NOT exist in dev-public (Kratos 401).
//
// Migrated from infra/tests/check-oidc-cred-admin-loopzero_test.sh (M-Migrate2, 2026-07-07).
// Hermetic: NO network, NO kubectl.
package oidccredadminloopzero

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/linescan"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

var forbiddenPattern = regexp.MustCompile(`paulo@loopzero\.test|LoopZeroDev2026!`)

var scanDirs = []string{
	"infra/tests/e2e",
	"infra/scripts/local",
}

type Config struct{ RepoRoot string }
type Outcome struct {
	Violations []string
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

// ErrNothingScanned means no scan dir exists, so nothing was read.
var ErrNothingScanned = errors.New("no scan dir exists — the gate read nothing")

// ErrViolations means a fabricated credential was found.
var ErrViolations = errors.New("fabricated paulo@loopzero.test/LoopZeroDev2026! credentials detected — canonical is admin@loopzero.example/loopzero-dev-password")

func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, err
		}
	}
	var violations []string
	walked, err := linescan.Walk(root, scanDirs, func(string) bool { return true }, func(rel string, data []byte) error {
		lines, err := linescan.Lines(data)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		for i, line := range lines {
			if forbiddenPattern.MatchString(line) {
				violations = append(violations, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
			}
		}
		return nil
	})
	if err != nil {
		return Outcome{}, err
	}
	if walked.Dirs == 0 {
		return Outcome{}, fmt.Errorf("%w: none of %s exists under %s", ErrNothingScanned, strings.Join(scanDirs, ", "), root)
	}
	if len(violations) > 0 {
		return Outcome{Violations: violations, Violation: ErrViolations}, nil
	}
	return Outcome{}, nil
}
