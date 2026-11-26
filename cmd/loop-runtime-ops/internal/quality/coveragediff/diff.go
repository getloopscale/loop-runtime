// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package coveragediff implements the diff-aware coverage gate per
// ADR D1: enforce coverage on NEW code only (lines added/modified by
// the PR), not global %. Replaces
// infra/scripts/quality/coverage-diff.sh (~128 LOC) per Phase 5 T5.13
// of plan 2026-05-21-bash-to-go-migration-plan.
//
// Origin: plan 2026-05-21-comprehensive-quality-gates v1.1 T2.1 + EC-7.
//
// Gate semantics (matches bash):
//   - Read PR-touched lines from a file in "path:line" format
//     (= output of cmd/loop-runtime-ops/internal/quality/gitdiff.FormatLines).
//   - Read merged lcov to get per-line hit counts.
//   - Filter touched lines to SOURCE files only (*.go *.ts *.tsx;
//     test files excluded).
//   - Compute (covered_source / total_source) ratio.
//   - PASS if no source lines OR ratio >= THRESHOLD (default 80%).
//   - EC-7: empty diff (rename-only / docs-only PR) → PASS. A MISSING
//     diff file is a gate error: the upstream step did not produce it.
package coveragediff

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/coverageparse"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// DefaultThreshold (matches bash 80%).
const DefaultThreshold = 80.0

// Config holds the gate inputs.
type Config struct {
	// DiffPath is a text file with one "path:line" per touched line
	// (= output of cmd/loop-runtime-ops/internal/quality/gitdiff.FormatLines).
	// Required.
	DiffPath string

	// LCOVPath is the merged lcov report (Go + CLI). Empty OR missing
	// file → all source lines counted as uncovered, and a message says so.
	// A file that exists but cannot be read or parsed is a gate error.
	LCOVPath string

	// Threshold percent — minimum coverage required on touched
	// source lines. Default 80.0.
	Threshold float64

	// ReportPath — optional Markdown report destination. Empty skips.
	ReportPath string
}

// Outcome captures the gate result.
type Outcome struct {
	// Pass is true when ratio >= Threshold OR no source lines touched
	// OR diff is empty (EC-7).
	Pass bool

	// TotalSource is the count of touched source-file lines.
	TotalSource int

	// CoveredSource is the subset of TotalSource with hit count > 0.
	CoveredSource int

	// Ratio is (covered / total) * 100, rounded to 1 decimal.
	// Zero when TotalSource is 0.
	Ratio float64

	// UncoveredLines is the list of "path:line" entries that should
	// be covered but aren't.
	UncoveredLines []string

	// SkipReason is non-empty when the gate was bypassed:
	//   "empty-diff"   — EC-7: rename-only / docs-only PR
	//   "no-source"    — diff has only test files or non-source paths
	SkipReason string

	// Messages is the ordered diagnostic stream.
	Messages []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// IsSourceFile returns true for `*.go`, `*.ts`, `*.tsx` files that
// are NOT test files (matches bash is_source_file).
func IsSourceFile(path string) bool {
	// Exclude test files first (bash check ordering).
	for _, suf := range []string{
		"_test.go", ".test.ts", ".spec.ts", ".test.tsx", ".spec.tsx",
	} {
		if strings.HasSuffix(path, suf) {
			return false
		}
	}
	// Include source extensions.
	for _, suf := range []string{".go", ".ts", ".tsx"} {
		if strings.HasSuffix(path, suf) {
			return true
		}
	}
	return false
}

// parseDiffLine splits a "path:line" entry into (path, lineNum).
// Returns ("", 0, false) on malformed input.
func parseDiffLine(entry string) (string, int, bool) {
	// Use LAST colon to split — file paths can contain colons in
	// theory (though unusual on POSIX).
	idx := strings.LastIndexByte(entry, ':')
	if idx <= 0 || idx == len(entry)-1 {
		return "", 0, false
	}
	path := entry[:idx]
	ln, err := strconv.Atoi(entry[idx+1:])
	if err != nil {
		return "", 0, false
	}
	return path, ln, true
}

// Run executes the gate end-to-end. Returns (*Outcome, error). Outcome
// drives caller exit code; error means the gate itself couldn't run.
//
// Errors:
//
//	OPS_USAGE          — invalid Config (missing DiffPath)
//	OPS_DEPLOY_FAILURE — diff file missing or unreadable, lcov unreadable
//	                     or unparseable, report not writable
func Run(cfg Config) (*Outcome, error) {
	if cfg.DiffPath == "" {
		return nil, &loopruntimeerrors.LoopRuntimeError{
			Code:    "OPS_USAGE",
			Message: "coveragediff: DiffPath required",
		}
	}
	threshold := cfg.Threshold
	if threshold == 0 {
		threshold = DefaultThreshold
	}

	// EC-7 is a diff that EXISTS and is empty (rename-only / docs-only PR).
	// A missing diff means the upstream step failed; that is a gate error.
	info, err := os.Stat(cfg.DiffPath)
	if err != nil {
		return nil, ioFailure(fmt.Sprintf("diff file %s is not readable — did the gitdiff step run?", cfg.DiffPath), err)
	}
	if info.Size() == 0 {
		return &Outcome{
			Pass:       true,
			SkipReason: "empty-diff",
			Messages: []string{
				"[coverage-diff] empty diff — PASS (rename-only or doc-only PR)",
			},
		}, nil
	}

	lineCov, lcovNote, err := loadLineCoverage(cfg.LCOVPath)
	if err != nil {
		return nil, err
	}
	out, err := classifyDiff(cfg.DiffPath, lineCov)
	if err != nil {
		return nil, err
	}
	if lcovNote != "" {
		out.Messages = append(out.Messages, lcovNote)
	}

	// No source lines → PASS (test-only or non-source PR).
	if out.TotalSource == 0 {
		out.Pass = true
		out.SkipReason = "no-source"
		out.Messages = append(out.Messages,
			"[coverage-diff] no source lines in diff — PASS (test-only or non-source PR)")
		return out, nil
	}

	out.Ratio = coverageparse.RoundTo1Decimal(float64(out.CoveredSource) / float64(out.TotalSource) * 100)
	out.Messages = append(out.Messages, fmt.Sprintf(
		"[coverage-diff] touched source lines: %d / covered: %d = %.1f%%",
		out.TotalSource, out.CoveredSource, out.Ratio,
	))

	if cfg.ReportPath != "" {
		if err := writeReport(cfg.ReportPath, out, threshold); err != nil {
			return out, err
		}
	}
	applyThreshold(out, threshold)
	return out, nil
}

// loadLineCoverage reads the lcov report. An empty path or a file that does
// not exist yields no coverage, so every touched source line counts as
// uncovered (the gate fails closed); the returned note says so. Any other
// read or parse failure is a gate error.
func loadLineCoverage(path string) (coverageparse.LineCoverage, string, error) {
	if path == "" {
		return nil, "", nil
	}
	f, err := os.Open(path) //nolint:gosec // caller-controlled
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Sprintf("[coverage-diff] lcov %s not found — every touched source line counts as uncovered", path), nil
	}
	if err != nil {
		return nil, "", ioFailure(fmt.Sprintf("cannot open lcov %s", path), err)
	}
	defer func() { _ = f.Close() }()
	lineCov, err := coverageparse.ParseLCOVLineCoverage(f)
	if err != nil {
		return nil, "", ioFailure("lcov parse failed", err)
	}
	return lineCov, "", nil
}

// classifyDiff walks the "path:line" diff file and counts touched source
// lines, covered or not.
func classifyDiff(diffPath string, lineCov coverageparse.LineCoverage) (*Outcome, error) {
	diffFile, err := os.Open(diffPath) //nolint:gosec // caller-controlled
	if err != nil {
		return nil, ioFailure(fmt.Sprintf("cannot open %s", diffPath), err)
	}
	defer func() { _ = diffFile.Close() }()

	out := &Outcome{}
	scanner := bufio.NewScanner(diffFile)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		path, ln, ok := parseDiffLine(strings.TrimSpace(scanner.Text()))
		if !ok || !IsSourceFile(path) {
			continue
		}
		out.TotalSource++
		if lineCov.IsCovered(path, ln) {
			out.CoveredSource++
		} else {
			out.UncoveredLines = append(out.UncoveredLines, fmt.Sprintf("%s:%d", path, ln))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, ioFailure("scanner error", err)
	}
	return out, nil
}

// applyThreshold sets Pass and appends the verdict messages.
func applyThreshold(out *Outcome, threshold float64) {
	if out.Ratio >= threshold {
		out.Pass = true
		out.Messages = append(out.Messages, fmt.Sprintf(
			"[coverage-diff] PASS (%.1f%% >= %.0f%%)", out.Ratio, threshold,
		))
		return
	}
	out.Pass = false
	out.Messages = append(out.Messages, fmt.Sprintf(
		"[coverage-diff] FAIL (%.1f%% < %.0f%%)", out.Ratio, threshold,
	))
	if len(out.UncoveredLines) > 0 {
		out.Messages = append(out.Messages, "[coverage-diff] uncovered lines:")
		for _, l := range out.UncoveredLines {
			out.Messages = append(out.Messages, "  "+l)
		}
	}
}

// ioFailure wraps a file I/O failure in the gate's typed error.
func ioFailure(what string, err error) error {
	return &loopruntimeerrors.LoopRuntimeError{
		Code:    "OPS_DEPLOY_FAILURE",
		Message: fmt.Sprintf("coveragediff: %s: %v", what, err),
		Cause:   err,
	}
}

// writeReport emits a Markdown report (matches bash REPORT_FILE block).
func writeReport(path string, out *Outcome, threshold float64) error {
	var sb strings.Builder
	sb.WriteString("# Coverage Diff Report\n\n")
	fmt.Fprintf(&sb, "- **Touched source lines:** %d\n", out.TotalSource)
	fmt.Fprintf(&sb, "- **Covered:** %d\n", out.CoveredSource)
	fmt.Fprintf(&sb, "- **Ratio:** %.1f%%\n", out.Ratio)
	fmt.Fprintf(&sb, "- **Threshold:** %.0f%%\n\n", threshold)
	if len(out.UncoveredLines) > 0 {
		sb.WriteString("## Uncovered lines\n\n")
		for _, l := range out.UncoveredLines {
			fmt.Fprintf(&sb, "- `%s`\n", l)
		}
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil { //nolint:gosec // report is operator-readable
		return &loopruntimeerrors.LoopRuntimeError{
			Code:    "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf("coveragediff: cannot write report %s: %v", path, err),
			Cause:   err,
		}
	}
	return nil
}

// WriteMessages emits Outcome.Messages to w, one per line.
func (o *Outcome) WriteMessages(w io.Writer) error {
	for _, m := range o.Messages {
		if _, err := fmt.Fprintln(w, m); err != nil {
			return err
		}
	}
	return nil
}
