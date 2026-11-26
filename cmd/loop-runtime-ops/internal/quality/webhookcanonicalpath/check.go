// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package webhookcanonicalpath enforces that no test/seed script references the
// wrong webhook path `/engine/api/v1/webhooks/github` — Caddy 301-redirects it
// to app-dev where there is no handler (404). The canonical is
// `/api/v1/webhooks/github` (handler-direct per Caddyfile lines 142-156).
//
// Migrated from infra/tests/check-webhook-canonical-path_test.sh (M-Migrate2, 2026-07-07).
// Hermetic: NO network, NO kubectl, NO helmfile — pure text walk over the working tree.
package webhookcanonicalpath

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/linescan"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// forbiddenPattern is the wrong URL that Caddy 301-redirects to app-dev where
// the /engine handler does not exist → 404 trap.
const forbiddenPattern = "/engine/api/v1/webhooks/github"

// scanDirs are the directory roots the original bash script walked. One missing
// dir is tolerated (archive/canonical-tests-v1 was DELETED in the M10 wave); all
// of them missing is ErrNothingScanned.
var scanDirs = []string{
	"infra/tests/e2e",
	"infra/scripts/local",
}

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the summary the CLI prints.
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

// ErrViolations means the forbidden pattern was found on at least one file.
var ErrViolations = errors.New("forbidden /engine/api/v1/webhooks/github pattern detected")

// Check walks scanDirs under repoRoot and reports ErrViolations, as the
// outcome's Violation, if any tracked
// file contains the forbiddenPattern. Comments-in-code are allowed via the
// `guard` / `legacy` marker on the same line (mirrors the sibling allowlist
// heuristic in T1.4).
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}

	var violations []string
	walked, err := linescan.Walk(root, scanDirs, func(string) bool { return true }, func(rel string, data []byte) error {
		lines, err := linescan.Lines(data)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		for i, line := range lines {
			if strings.Contains(line, forbiddenPattern) && !isAllowlistedComment(line) {
				violations = append(violations, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
			}
		}
		return nil
	})
	if err != nil {
		return Outcome{}, err
	}
	// One dir may be gone (strangler waves deleted some); all of them gone means
	// nothing was read, which is not a clean result.
	if walked.Dirs == 0 {
		return Outcome{}, fmt.Errorf("%w: none of %s exists under %s", ErrNothingScanned, strings.Join(scanDirs, ", "), root)
	}

	if len(violations) > 0 {
		return Outcome{Violations: violations, Violation: ErrViolations}, nil
	}
	return Outcome{}, nil
}

// isAllowlistedComment allows lines that are documenting the forbidden pattern
// as a guard/legacy/forbidden reference (audit-trail comments in other checks
// pointing at this same pattern). Mirrors the check-oidc-cred-admin-loop-runtime
// pattern.
func isAllowlistedComment(line string) bool {
	lower := strings.ToLower(line)
	if !strings.Contains(strings.TrimSpace(lower), "#") && !strings.Contains(strings.TrimSpace(lower), "//") {
		return false
	}
	for _, marker := range []string{"guard", "legacy", "deprecated", "superseded", "forbidden"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
