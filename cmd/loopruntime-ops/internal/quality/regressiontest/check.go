// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package regressiontest implements the bug-fix regression-test gate: a PR
// that references a bug/issue (fix #N, closes #N, Bug #N, ...) MUST add at
// least one test file in its diff.
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3:
// replaces infra/scripts/quality/check-regression-test.sh. Pure-Go (regexp +
// string scan) — no external CLI. Genuine pure-logic win per D5.
package regressiontest

import (
	"regexp"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

// Config drives the gate (all from PR context env vars).
type Config struct {
	Title     string // PR_TITLE
	Body      string // PR_BODY
	Commits   string // PR_COMMITS (newline-separated)
	DiffFiles string // PR_DIFF_FILES (newline-separated)
}

// Outcome captures the result.
type Outcome struct {
	Pass      bool
	Skipped   bool
	IssueRefs []string
	Reason    string
}

// Verdict is Skip when the subject is absent from this checkout, and
// otherwise Pass when the check passed and Fail when it did not.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Skipped {
		return verdict.Skip
	}
	return verdict.FromPass(o.Pass)
}

// issueRefRe matches a bug/issue keyword immediately followed by #N.
// Keywords (case-insensitive): fix(es|ed), close(s|d), resolve(s|d), Bug.
var issueRefRe = regexp.MustCompile(`(?i)(fix|fixes|fixed|close|closes|closed|resolve|resolves|resolved|bug)\s*#([0-9]+)`)

// Check runs the gate.
func Check(cfg Config) Outcome {
	// Source priority per EC-3: title, body, commits.
	combined := cfg.Title + "\n" + cfg.Body + "\n" + cfg.Commits
	refs := extractIssueRefs(combined)

	if len(refs) == 0 {
		return Outcome{Pass: true, Skipped: true, Reason: "no bug-fix references — gate skipped"}
	}

	for _, f := range strings.Split(cfg.DiffFiles, "\n") {
		if isTestFile(strings.TrimSpace(f)) {
			return Outcome{Pass: true, IssueRefs: refs, Reason: "bug-fix PR includes a test file"}
		}
	}

	return Outcome{
		Pass:      false,
		IssueRefs: refs,
		Reason:    "bug-fix PR (refs " + strings.Join(refs, ",") + ") has no new test file in diff",
	}
}

// extractIssueRefs returns the unique, order-preserved set of #N references
// that follow a bug/issue keyword.
func extractIssueRefs(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range issueRefRe.FindAllStringSubmatch(text, -1) {
		ref := "#" + m[2]
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out
}

// isTestFile classifies a path as a test file by extension.
func isTestFile(f string) bool {
	for _, suffix := range []string{"_test.go", ".test.ts", ".spec.ts", ".test.tsx", ".spec.tsx"} {
		if strings.HasSuffix(f, suffix) {
			return true
		}
	}
	return false
}
