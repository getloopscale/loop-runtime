// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package coveragemerge converts a Go coverprofile to lcov format and
// concatenates it with a TypeScript lcov to produce one unified lcov
// consumable by downstream tools (codecov, diff-cover, baseline-update).
// Replaces infra/scripts/quality/coverage-merge.sh (~102 LOC) per Phase
// 5 T5.11 of plan 2026-05-21-bash-to-go-migration-plan.
//
// Origin: plan 2026-05-21-comprehensive-quality-gates v1.1 T1.1.
//
// Go profile line format (post-header):
//
//	<file>:<sLine>.<sCol>,<eLine>.<eCol> <stmts> <count>
//
// Emitted lcov per file:
//
//	SF:<file>
//	DA:<line>,<count>      # one per line in [sLine, eLine]
//	LF:<lines_found>
//	LH:<lines_hit>
//	end_of_record
//
// FAANG-grade fix vs bash: bash awk assumes the profile is grouped by
// file (it flushes SF/LF/LH only when `file != last_file`). When the
// same file appears in non-consecutive blocks, bash emits duplicate
// SF records for it — confusing downstream parsers. This implementation
// accumulates per-file across all blocks and emits ONE record per file.
// When the same line appears in overlapping blocks, max(count) wins.
package coveragemerge

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/coverageparse"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// Config holds the merger inputs.
type Config struct {
	// GoProfilePath is the Go coverage profile (`go test -coverprofile`).
	// Empty or missing → skipped silently (bash parity).
	GoProfilePath string

	// CLILCOVPath is the pre-existing TypeScript lcov.
	// Empty or missing → skipped silently.
	CLILCOVPath string

	// OutputPath is the merged lcov destination. Required.
	OutputPath string
}

// Outcome captures the merge result.
type Outcome struct {
	// Files is the number of SF records in the output.
	Files int

	// Lines is the total number of lines written to the output.
	Lines int

	// Empty is true when neither input produced any content.
	Empty bool

	// Messages — diagnostic stream.
	Messages []string
}

// Verdict is Pass: merging is an operation, and an Outcome exists only when
// it completed. A merge that failed is Merge's error.
func (o Outcome) Verdict() verdict.Verdict { return verdict.Pass }

// fileLines accumulates per-line counts for a single source file.
type fileLines struct {
	// counts maps line-number → max observed hit count.
	counts map[int]int
	// order tracks first-encountered line numbers for deterministic emission.
	order []int
}

func newFileLines() *fileLines {
	return &fileLines{counts: map[int]int{}}
}

// observe records a hit count for a specific line, keeping the max
// when the same line is seen in overlapping blocks.
func (fl *fileLines) observe(line, count int) {
	if prev, ok := fl.counts[line]; ok {
		if count > prev {
			fl.counts[line] = count
		}
		return
	}
	fl.counts[line] = count
	fl.order = append(fl.order, line)
}

// ConvertGoProfile reads a Go cover profile from r and writes the
// equivalent lcov body to w. Returns (filesEmitted, linesEmitted, err).
//
// Behavior:
//   - First line (mode header) is skipped.
//   - Malformed lines are silently skipped (matches bash awk).
//   - Files are emitted in first-encounter order.
//   - Lines within a file are emitted in ascending order.
func ConvertGoProfile(r io.Reader, w io.Writer) (int, int, error) {
	files := map[string]*fileLines{}
	var fileOrder []string

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	headerSkipped := false
	for scanner.Scan() {
		raw := scanner.Text()
		if !headerSkipped {
			headerSkipped = true
			continue
		}
		b, ok := coverageparse.ParseProfileLine(raw)
		if !ok {
			continue
		}
		file, sLine, eLine, count := b.File, b.StartLine, b.EndLine, b.Count
		fl, exists := files[file]
		if !exists {
			fl = newFileLines()
			files[file] = fl
			fileOrder = append(fileOrder, file)
		}
		for ln := sLine; ln <= eLine; ln++ {
			fl.observe(ln, count)
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}

	emittedFiles := 0
	emittedLines := 0
	for _, file := range fileOrder {
		fl := files[file]
		if _, err := fmt.Fprintf(w, "SF:%s\n", file); err != nil {
			return emittedFiles, emittedLines, err
		}
		emittedLines++
		// Stable per-line emission.
		sort.Ints(fl.order)
		lf := 0
		lh := 0
		seen := map[int]bool{}
		for _, ln := range fl.order {
			if seen[ln] {
				continue
			}
			seen[ln] = true
			count := fl.counts[ln]
			if _, err := fmt.Fprintf(w, "DA:%d,%d\n", ln, count); err != nil {
				return emittedFiles, emittedLines, err
			}
			emittedLines++
			lf++
			if count > 0 {
				lh++
			}
		}
		if _, err := fmt.Fprintf(w, "LF:%d\nLH:%d\nend_of_record\n", lf, lh); err != nil {
			return emittedFiles, emittedLines, err
		}
		emittedLines += 3
		emittedFiles++
	}
	return emittedFiles, emittedLines, nil
}

// fileNonEmpty returns true when path exists AND has non-zero size.
func fileNonEmpty(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Size() > 0
}

// Merge orchestrates the full producer flow: convert Go profile, append
// CLI lcov, write to OutputPath, return diagnostic Outcome.
func Merge(cfg Config) (*Outcome, error) {
	if cfg.OutputPath == "" {
		return nil, &loopzeroerrors.LoopZeroError{
			Code:    "OPS_USAGE",
			Message: "coveragemerge: OutputPath required",
		}
	}
	if err := os.MkdirAll(filepath.Dir(cfg.OutputPath), 0o750); err != nil {
		return nil, &loopzeroerrors.LoopZeroError{
			Code:    "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf("coveragemerge: cannot create output dir: %v", err),
			Cause:   err,
		}
	}

	var buf strings.Builder
	out := &Outcome{}

	// Go profile.
	if fileNonEmpty(cfg.GoProfilePath) {
		f, err := os.Open(cfg.GoProfilePath) //nolint:gosec // caller-controlled
		if err != nil {
			return nil, &loopzeroerrors.LoopZeroError{
				Code:    "OPS_USAGE",
				Message: fmt.Sprintf("coveragemerge: cannot open Go profile %s: %v", cfg.GoProfilePath, err),
				Cause:   err,
			}
		}
		files, lines, convErr := ConvertGoProfile(f, &buf)
		_ = f.Close()
		if convErr != nil {
			return nil, &loopzeroerrors.LoopZeroError{
				Code:    "OPS_DEPLOY_FAILURE",
				Message: fmt.Sprintf("coveragemerge: convert Go profile failed: %v", convErr),
				Cause:   convErr,
			}
		}
		out.Files += files
		out.Lines += lines
	} else {
		out.Messages = append(out.Messages, fmt.Sprintf(
			"[coverage-merge] Go profile %s missing/empty — skipping",
			cfg.GoProfilePath))
	}

	// CLI lcov (concat verbatim).
	if fileNonEmpty(cfg.CLILCOVPath) {
		body, err := os.ReadFile(cfg.CLILCOVPath) //nolint:gosec // caller-controlled
		if err != nil {
			return nil, &loopzeroerrors.LoopZeroError{
				Code:    "OPS_USAGE",
				Message: fmt.Sprintf("coveragemerge: cannot read CLI lcov %s: %v", cfg.CLILCOVPath, err),
				Cause:   err,
			}
		}
		buf.Write(body)
		// Recount additions from the appended bytes.
		cliFiles, cliLines := countLCOVStats(body)
		out.Files += cliFiles
		out.Lines += cliLines
	}

	merged := buf.String()
	if merged == "" {
		out.Empty = true
		out.Messages = append(out.Messages,
			"[coverage-merge] WARN: merged output empty (no Go or CLI coverage available)")
		// Still write the empty file so downstream consumers see it.
		if err := os.WriteFile(cfg.OutputPath, []byte{}, 0o600); err != nil {
			return nil, &loopzeroerrors.LoopZeroError{
				Code:    "OPS_DEPLOY_FAILURE",
				Message: fmt.Sprintf("coveragemerge: cannot write empty output: %v", err),
				Cause:   err,
			}
		}
		return out, nil
	}

	if err := os.WriteFile(cfg.OutputPath, []byte(merged), 0o600); err != nil {
		return nil, &loopzeroerrors.LoopZeroError{
			Code:    "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf("coveragemerge: cannot write %s: %v", cfg.OutputPath, err),
			Cause:   err,
		}
	}
	out.Messages = append(out.Messages, fmt.Sprintf(
		"[coverage-merge] merged: %d files / %d lines → %s",
		out.Files, out.Lines, cfg.OutputPath))
	return out, nil
}

// countLCOVStats returns (sfRecords, totalLines) from raw lcov bytes.
// Matches bash `grep -c '^SF:' + wc -l` semantic.
func countLCOVStats(body []byte) (int, int) {
	files := 0
	lines := 0
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		lines++
		if strings.HasPrefix(scanner.Text(), "SF:") {
			files++
		}
	}
	return files, lines
}
