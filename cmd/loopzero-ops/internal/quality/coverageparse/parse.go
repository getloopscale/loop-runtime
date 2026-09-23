// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package coverageparse extracts per-package coverage percentages from
// Go coverage profiles and TS/JS lcov reports. Foundation for Phase 5
// T5.16 baseline-update (also reusable by T5.11 coverage-merge +
// T5.13 coverage-diff).
//
// Replaces the awk one-liners in infra/scripts/quality/baseline-update.sh
// per Phase 5 of plan
// 2026-05-21-bash-to-go-migration-plan.
//
// Go profile format (one statement-range per data line after header):
//
//	mode: atomic
//	github.com/getloopscale/loopzero/pkg/foo/bar.go:10.15,20.2 5 3
//	github.com/getloopscale/loopzero/pkg/foo/bar.go:25.3,28.4 2 0
//
// Per-package coverage = sum(stmts where count>0) / sum(stmts).
//
// lcov format (subset used):
//
//	SF:src/cli/auth/login.ts
//	... DA/BRDA records ...
//	LF:100         (lines found)
//	LH:80          (lines hit)
//	end_of_record
//
// Per-package coverage = mean of per-file (LH/LF * 100).
package coverageparse

import (
	"bufio"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// PackageCoverage is one package's measured coverage percentage.
type PackageCoverage struct {
	Package  string
	Coverage float64 // 0..100, one decimal place per bash output
}

// ParseGoProfile reads a `go tool cover -profile` output stream and
// returns per-package coverage percentages, sorted alphabetically.
//
// Bash equivalent: awk parser in baseline-update.sh
// compute_go_coverage_per_package().
func ParseGoProfile(r io.Reader) ([]PackageCoverage, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// fileStmts[file] = total statements; fileCovered[file] = covered.
	fileStmts := map[string]int{}
	fileCovered := map[string]int{}

	header := true
	for scanner.Scan() {
		line := scanner.Text()
		if header {
			header = false
			// First line is `mode: atomic|count|set` — skip it.
			continue
		}
		if line == "" {
			continue
		}
		b, ok := ParseProfileLine(line)
		if !ok {
			// Malformed — skip (matches awk `next`).
			continue
		}
		file, stmts := b.File, b.Stmts
		fileStmts[file] += stmts
		if b.Count > 0 {
			fileCovered[file] += stmts
		} else {
			// Ensure key exists (matches awk semantic for files with no
			// covered statements).
			if _, ok := fileCovered[file]; !ok {
				fileCovered[file] = 0
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, &loopzeroerrors.LoopZeroError{
			Code:    "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf("coverageparse: scanner error: %v", err),
			Cause:   err,
		}
	}

	// Aggregate per-package (dirname of file).
	pkgStmts := map[string]int{}
	pkgCovered := map[string]int{}
	for f, stmts := range fileStmts {
		pkg := pkgFromFile(f)
		if pkg == "" {
			continue
		}
		pkgStmts[pkg] += stmts
		pkgCovered[pkg] += fileCovered[f]
	}

	out := make([]PackageCoverage, 0, len(pkgStmts))
	for pkg, total := range pkgStmts {
		pct := 0.0
		if total > 0 {
			pct = float64(pkgCovered[pkg]) / float64(total) * 100
		}
		out = append(out, PackageCoverage{
			Package:  pkg,
			Coverage: RoundTo1Decimal(pct),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Package < out[j].Package })
	return out, nil
}

// pkgFromFile returns the directory portion (everything before the
// last '/') — the awk equivalent in baseline-update.sh.
func pkgFromFile(file string) string {
	dir := path.Dir(file)
	if dir == "." {
		// `file` has no slash → no package association.
		return ""
	}
	return dir
}

// ProfileBlock is one line of a Go cover profile.
type ProfileBlock struct {
	File      string
	StartLine int
	EndLine   int
	Stmts     int
	Count     int
}

// ParseProfileLine parses `<file>:<sLine>.<sCol>,<eLine>.<eCol> <stmts> <count>`.
// The file is split at the LAST colon: the range carries none, and a Windows
// path carries one of its own. It is the single parser for the format; the
// coverage gates that read profiles must not keep their own.
func ParseProfileLine(line string) (ProfileBlock, bool) {
	colon := strings.LastIndexByte(line, ':')
	if colon <= 0 {
		return ProfileBlock{}, false
	}
	fields := strings.Fields(line[colon+1:])
	if len(fields) < 3 {
		return ProfileBlock{}, false
	}
	start, end, ok := strings.Cut(fields[0], ",")
	if !ok {
		return ProfileBlock{}, false
	}
	sLine, ok1 := lineOf(start)
	eLine, ok2 := lineOf(end)
	stmts, err1 := strconv.Atoi(fields[1])
	count, err2 := strconv.Atoi(fields[2])
	if !ok1 || !ok2 || err1 != nil || err2 != nil || sLine < 1 || eLine < sLine {
		return ProfileBlock{}, false
	}
	return ProfileBlock{File: line[:colon], StartLine: sLine, EndLine: eLine, Stmts: stmts, Count: count}, true
}

// lineOf returns the line of a `<line>.<col>` position.
func lineOf(pos string) (int, bool) {
	l, _, ok := strings.Cut(pos, ".")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(l)
	return n, err == nil
}

// RoundTo1Decimal preserves the bash `printf "%.1f"` output precision.
// Avoids float drift on long sums (87.5499... → 87.5 not 87.6).
func RoundTo1Decimal(f float64) float64 {
	rounded, _ := strconv.ParseFloat(fmt.Sprintf("%.1f", f), 64)
	return rounded
}

// LineCoverage maps file → line number → hit count.
// hit count > 0 means the line is covered; 0 means uncovered.
type LineCoverage map[string]map[int]int

// IsCovered returns true when (file, line) is recorded with hit count > 0.
// Files NOT in the map (e.g. untouched by tests) → false.
func (l LineCoverage) IsCovered(file string, line int) bool {
	if fileMap, ok := l[file]; ok {
		return fileMap[line] > 0
	}
	return false
}

// ParseLCOVLineCoverage reads an lcov-format report and returns
// per-FILE per-LINE hit counts. Different aggregation from ParseLCOV
// (which sums to per-package). Both share the lcov format but answer
// different questions:
//   - ParseLCOV → "what's the package coverage %?" (T5.16 baseline-update)
//   - ParseLCOVLineCoverage → "is THIS specific line covered?" (T5.13 coverage-diff)
//
// Bash equivalent: the COVERED_LINES associative array build in
// coverage-diff.sh (DA:<line>,<count> per file).
func ParseLCOVLineCoverage(r io.Reader) (LineCoverage, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	out := LineCoverage{}
	var curFile string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "SF:"):
			curFile = strings.TrimPrefix(line, "SF:")
			if _, ok := out[curFile]; !ok {
				out[curFile] = map[int]int{}
			}
		case strings.HasPrefix(line, "DA:"):
			// DA:<line>,<hitCount>
			rest := strings.TrimPrefix(line, "DA:")
			commaIdx := strings.IndexByte(rest, ',')
			if commaIdx <= 0 || curFile == "" {
				continue
			}
			ln, err1 := strconv.Atoi(rest[:commaIdx])
			cnt, err2 := strconv.Atoi(rest[commaIdx+1:])
			if err1 != nil || err2 != nil {
				continue
			}
			// Record max-of-current-and-new to handle repeated DA entries
			// (lcov may emit multiple records for the same line; pick the
			// max hit count — most permissive).
			if existing, ok := out[curFile][ln]; !ok || cnt > existing {
				out[curFile][ln] = cnt
			}
		case line == "end_of_record":
			curFile = ""
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, &loopzeroerrors.LoopZeroError{
			Code:    "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf("coverageparse: lcov line-coverage scanner error: %v", err),
			Cause:   err,
		}
	}
	return out, nil
}

// ParseLCOV reads an lcov-format coverage report and returns per-package
// coverage percentages (mean of per-file LH/LF * 100), sorted
// alphabetically.
//
// Bash equivalent: awk parser in baseline-update.sh
// compute_cli_coverage_per_package().
func ParseLCOV(r io.Reader) ([]PackageCoverage, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	pkgSum := map[string]float64{}
	pkgCount := map[string]int{}

	var curFile string
	var lf, lh int
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "SF:"):
			curFile = strings.TrimPrefix(line, "SF:")
			lf, lh = 0, 0
		case strings.HasPrefix(line, "LF:"):
			lf, _ = strconv.Atoi(strings.TrimPrefix(line, "LF:"))
		case strings.HasPrefix(line, "LH:"):
			lh, _ = strconv.Atoi(strings.TrimPrefix(line, "LH:"))
		case line == "end_of_record":
			if curFile == "" {
				continue
			}
			pkg := pkgFromFile(curFile)
			if pkg == "" {
				curFile = ""
				continue
			}
			pct := 0.0
			if lf > 0 {
				pct = float64(lh) / float64(lf) * 100
			}
			pkgSum[pkg] += pct
			pkgCount[pkg]++
			curFile = ""
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, &loopzeroerrors.LoopZeroError{
			Code:    "OPS_DEPLOY_FAILURE",
			Message: fmt.Sprintf("coverageparse: lcov scanner error: %v", err),
			Cause:   err,
		}
	}

	out := make([]PackageCoverage, 0, len(pkgSum))
	for pkg, sum := range pkgSum {
		avg := sum / float64(pkgCount[pkg])
		out = append(out, PackageCoverage{
			Package:  pkg,
			Coverage: RoundTo1Decimal(avg),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Package < out[j].Package })
	return out, nil
}
