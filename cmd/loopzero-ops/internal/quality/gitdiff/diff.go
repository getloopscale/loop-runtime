// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package gitdiff parses unified-diff output into per-line (file, line)
// pairs that downstream quality gates use to scope their checks to
// only the lines added or modified in a PR. Replaces
// infra/scripts/quality/git-diff-to-lines.sh (~24 LOC) per Phase 5
// T5.2 of plan 2026-05-21-bash-to-go-migration-plan.
//
// Origin: plan 2026-05-21-comprehensive-quality-gates v1.1 T2.1.
//
// Consumed by Phase 5 downstream gates:
//   - T5.4 check-test-deletion  (PR drops a test file)
//   - T5.7 check-regression-test (bug-fix PR must add a regression test)
//   - T5.13 coverage-diff       (lines added must be covered)
//   - T5.14 check-skip-link     (skipped tests must reference an issue)
//
// Bash version pipes `git diff --unified=0 BASE...HEAD` to a python
// inline-script that grabs `+++ b/FILE` headers + `@@ -X,Y +A,B @@`
// hunk hints and emits `FILE:LINE` per line in the +B range.
// Native Go version removes the python runtime dep and exposes the
// pairs as a typed slice for downstream consumers.
package gitdiff

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/shellrun"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// Pair is a single added-or-modified line, keyed by file path.
type Pair struct {
	File string
	Line int
}

// String returns "FILE:LINE" — matches the bash script output for
// callers that pipe it to grep/wc.
func (p Pair) String() string {
	return fmt.Sprintf("%s:%d", p.File, p.Line)
}

// Pairs is the listing of added-or-modified lines a diff produced.
type Pairs []Pair

// Verdict is Pass: the listing is an operation, and a Pairs exists only when
// the diff was read. A diff that could not be read is RunGitDiff's error.
func (Pairs) Verdict() verdict.Verdict { return verdict.Pass }

// Config holds the diff-range inputs.
type Config struct {
	// BaseRef defaults to "origin/main".
	BaseRef string

	// HeadRef defaults to "HEAD".
	HeadRef string
}

// fileHeader matches the `+++ b/path/to/file.go` line of a unified diff.
var fileHeader = regexp.MustCompile(`^\+\+\+ b/(.+)`)

// hunkHeader matches the `@@ -X,Y +A,B @@` line; captures the +A start
// + optional ,B count (default 1 when omitted).
var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))?`)

// Parse reads a unified-diff stream and returns one Pair per
// added-or-modified line. Mirrors the python script byte-for-byte
// semantics: files without a hunk yield zero pairs; deletions
// (count=0 in +A,0) yield zero pairs.
func Parse(r io.Reader) ([]Pair, error) {
	scanner := bufio.NewScanner(r)
	// Diff hunks can have very long context lines; allow up to 1 MiB
	// per line (default 64 KiB blows up on minified JS/CSS).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var pairs []Pair
	var current string
	for scanner.Scan() {
		line := scanner.Text()
		if m := fileHeader.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		if current == "" {
			continue
		}
		if m := hunkHeader.FindStringSubmatch(line); m != nil {
			start, err := strconv.Atoi(m[1])
			if err != nil {
				return nil, &loopzeroerrors.LoopZeroError{
					Code:    "OPS_USAGE",
					Message: fmt.Sprintf("gitdiff: invalid hunk start in %q: %v", line, err),
					Cause:   err,
				}
			}
			count := 1
			if m[2] != "" {
				count, err = strconv.Atoi(m[2])
				if err != nil {
					return nil, &loopzeroerrors.LoopZeroError{
						Code:    "OPS_USAGE",
						Message: fmt.Sprintf("gitdiff: invalid hunk count in %q: %v", line, err),
						Cause:   err,
					}
				}
			}
			if count == 0 {
				// Pure deletion hunk — no added lines to track.
				continue
			}
			for i := 0; i < count; i++ {
				pairs = append(pairs, Pair{File: current, Line: start + i})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, &loopzeroerrors.LoopZeroError{
			Code:    "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf("gitdiff: scanner error: %v", err),
			Cause:   err,
		}
	}
	return pairs, nil
}

// RunGitDiff invokes `git diff --unified=0 BASE...HEAD` via the runner
// and parses the output. Equivalent to the bash one-liner:
//
//	git diff --unified=0 "${BASE_REF}...${HEAD_REF}" | python3 -c '...'
//
// Errors:
//
//	OPS_USAGE          — invalid hunk header parse
//	OPS_DEPLOY_FAILURE — git invocation failed
func RunGitDiff(ctx context.Context, runner shellrun.Runner, cfg Config) (Pairs, error) {
	baseRef := cfg.BaseRef
	if baseRef == "" {
		baseRef = "origin/main"
	}
	headRef := cfg.HeadRef
	if headRef == "" {
		headRef = "HEAD"
	}
	res, err := runner.Run(ctx, "git",
		[]string{"diff", "--unified=0", baseRef + "..." + headRef})
	if err != nil {
		return nil, &loopzeroerrors.LoopZeroError{
			Code: "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf(
				"gitdiff: `git diff --unified=0 %s...%s` failed", baseRef, headRef,
			),
			Cause: err,
		}
	}
	if res == nil {
		return nil, nil
	}
	return Parse(strings.NewReader(res.Stdout))
}

// FormatLines returns one "FILE:LINE" entry per pair, joined by newlines.
// Exposed for callers that need to pipe through grep/wc + downstream tools
// expecting the bash output format.
func FormatLines(pairs []Pair) string {
	var sb strings.Builder
	for _, p := range pairs {
		sb.WriteString(p.String())
		sb.WriteByte('\n')
	}
	return sb.String()
}
