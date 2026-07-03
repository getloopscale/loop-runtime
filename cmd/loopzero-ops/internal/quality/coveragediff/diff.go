// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package coveragediff implements the diff-aware coverage gate per
// ADR D1: enforce coverage on NEW code only (lines added/modified by
// the PR), not global %. Replaces
// infra/scripts/quality/coverage-diff.sh (~128 LOC) per Phase 5 T5.13
// of plan docs/plans/completed/2026-05-21-bash-to-go-migration-plan.md.
//
// Origin: plan 2026-05-21-comprehensive-quality-gates v1.1 T2.1 + EC-7.
//
// Gate semantics (matches bash):
//   - Read PR-touched lines from a file in "path:line" format
//     (= output of pkg/quality/gitdiff.FormatLines).
//   - Read merged lcov to get per-line hit counts.
//   - Filter touched lines to SOURCE files only (*.go *.ts *.tsx;
//     test files excluded).
//   - Compute (covered_source / total_source) ratio.
//   - PASS if no source lines OR ratio >= THRESHOLD (default 80%).
//   - EC-7: empty diff (rename-only / docs-only PR) → PASS.
package coveragediff

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/coverageparse"
)

// DefaultThreshold (matches bash 80%).
const DefaultThreshold = 80.0

// Config holds the gate inputs.
type Config struct {
	// DiffPath is a text file with one "path:line" per touched line
	// (= output of pkg/quality/gitdiff.FormatLines).
	// Required.
	DiffPath string

	// LCOVPath is the merged lcov report (Go + CLI). Empty OR missing
	// file → all source lines counted as uncovered.
	// Required for meaningful results, but missing → silent-skip
	// matches bash semantic.
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
	// loopzerory (though unusual on POSIX).
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
// drives caller exit code; error means the gate itself couldn't run
// (missing diff file in non-EC-7 path).
//
// Errors:
//
//	OPS_USAGE          — invalid Config (missing DiffPath)
//	OPS_DEPLOY_FAILURE — file I/O OR lcov parse failure
func Run(cfg Config) (*Outcome, error) {
	if cfg.DiffPath == "" {
		return nil, &loopzeroerrors.LoopZeroError{
			Code:    "OPS_USAGE",
			Message: "coveragediff: DiffPath required",
		}
	}
	threshold := cfg.Threshold
	if threshold == 0 {
		threshold = DefaultThreshold
	}

	// EC-7: empty/missing diff → PASS (rename-only / docs-only PR).
	info, err := os.Stat(cfg.DiffPath)
	if err != nil || info.Size() == 0 {
		return &Outcome{
			Pass:       true,
			SkipReason: "empty-diff",
			Messages: []string{
				"[coverage-diff] empty diff — PASS (rename-only or doc-only PR)",
			},
		}, nil
	}

	// Load LCOV line coverage (optional — missing file → empty map).
	var lineCov coverageparse.LineCoverage
	if cfg.LCOVPath != "" {
		if f, openErr := os.Open(cfg.LCOVPath); openErr == nil {
			lineCov, err = coverageparse.ParseLCOVLineCoverage(f)
			_ = f.Close()
			if err != nil {
				return nil, &loopzeroerrors.LoopZeroError{
					Code:    "OPS_DEPLOY_FAILURE",
					Message: fmt.Sprintf("coveragediff: lcov parse failed: %v", err),
					Cause:   err,
				}
			}
		}
		// Missing lcov file: silent skip; lineCov stays nil → IsCovered
		// always returns false → every touched source line counted as
		// uncovered (matches bash empty-LCOV behaviour).
	}

	// Walk diff file + classify each line.
	diffFile, err := os.Open(cfg.DiffPath) //nolint:gosec // caller-controlled
	if err != nil {
		return nil, &loopzeroerrors.LoopZeroError{
			Code:    "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf("coveragediff: cannot open %s: %v", cfg.DiffPath, err),
			Cause:   err,
		}
	}
	defer func() { _ = diffFile.Close() }()

	out := &Outcome{}
	scanner := bufio.NewScanner(diffFile)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		entry := strings.TrimSpace(scanner.Text())
		if entry == "" {
			continue
		}
		path, ln, ok := parseDiffLine(entry)
		if !ok {
			continue
		}
		if !IsSourceFile(path) {
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
		return nil, &loopzeroerrors.LoopZeroError{
			Code:    "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf("coveragediff: scanner error: %v", err),
			Cause:   err,
		}
	}

	// No source lines → PASS (test-only or non-source PR).
	if out.TotalSource == 0 {
		out.Pass = true
		out.SkipReason = "no-source"
		out.Messages = []string{
			"[coverage-diff] no source lines in diff — PASS (test-only or non-source PR)",
		}
		return out, nil
	}

	// Compute ratio (rounded to 1 decimal).
	rawRatio := float64(out.CoveredSource) / float64(out.TotalSource) * 100
	out.Ratio = roundTo1Decimal(rawRatio)

	out.Messages = append(out.Messages, fmt.Sprintf(
		"[coverage-diff] touched source lines: %d / covered: %d = %.1f%%",
		out.TotalSource, out.CoveredSource, out.Ratio,
	))

	// Optional report file.
	if cfg.ReportPath != "" {
		if err := writeReport(cfg.ReportPath, out, threshold); err != nil {
			return out, err
		}
	}

	// Threshold check.
	if out.Ratio >= threshold {
		out.Pass = true
		out.Messages = append(out.Messages, fmt.Sprintf(
			"[coverage-diff] PASS (%.1f%% >= %.0f%%)", out.Ratio, threshold,
		))
		return out, nil
	}

	// FAIL.
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
	return out, nil
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
		return &loopzeroerrors.LoopZeroError{
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

// roundTo1Decimal rounds float to 1 decimal place via printf+ParseFloat.
func roundTo1Decimal(f float64) float64 {
	rounded, _ := strconv.ParseFloat(fmt.Sprintf("%.1f", f), 64)
	return rounded
}
