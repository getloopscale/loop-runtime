// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package jobtimeout is the Go home for the job-timeout-coverage gate
// (migrated from infra/tests/workflows/job-timeout-coverage_test.sh, plan
// 2026-05-23 operational-logic capability architecture).
//
// It enforces that every job in .github/workflows/*.yml sets job-level
// timeout-minutes, EXCEPT reusable-workflow caller jobs (`uses: ./.github/...`)
// which inherit timeout from the called workflow — GitHub Actions rejects
// timeout-minutes on `uses:` jobs.
//
// Pure line-based parsing (faithful to the original Python heredoc): a full
// YAML parse is overkill and would lose the line numbers the gate reports.
package jobtimeout

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Config tells Check where the workflows live.
type Config struct {
	WorkflowsDir string // path to .github/workflows
}

// Outcome is the gate result.
type Outcome struct {
	Pass            bool
	Total           int
	Covered         int
	ReusableCallers int
	Violations      []string
}

var (
	// jobKeyRe matches a job declaration: exactly 2-space indent, identifier, colon.
	jobKeyRe = regexp.MustCompile(`^  ([a-zA-Z][a-zA-Z0-9_-]*):\s*$`)
	// topLevelKeyRe matches a column-0 key (ends the jobs section).
	topLevelKeyRe = regexp.MustCompile(`^[a-zA-Z]`)
	// jobTimeoutRe matches a job-level (4-space) timeout-minutes.
	jobTimeoutRe = regexp.MustCompile(`^    timeout-minutes:`)
	// reusableCallerRe matches a job that calls a reusable workflow.
	reusableCallerRe = regexp.MustCompile(`^    uses:\s*\./\.github/workflows/`)
)

// Check scans every .github/workflows/*.yml and reports jobs missing a
// job-level timeout-minutes (reusable-workflow callers exempt).
func Check(cfg Config) (*Outcome, error) {
	entries, err := os.ReadDir(cfg.WorkflowsDir)
	if err != nil {
		return nil, fmt.Errorf("read workflows dir %q: %w", cfg.WorkflowsDir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)

	out := &Outcome{Pass: true}
	for _, name := range files {
		if err := scanWorkflow(filepath.Join(cfg.WorkflowsDir, name), name, out); err != nil {
			return nil, err
		}
	}
	out.Pass = len(out.Violations) == 0
	return out, nil
}

// scanWorkflow parses one workflow file, accumulating job tallies + violations.
func scanWorkflow(absPath, displayName string, out *Outcome) error {
	// #nosec G304 -- absPath comes from os.ReadDir of the configured dir.
	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", displayName, err)
	}

	var (
		inJobs         bool
		curJob         string
		curLine        int
		hasTimeout     bool
		isReusableCall bool
	)
	finalize := func() {
		if curJob == "" {
			return
		}
		out.Total++
		switch {
		case isReusableCall:
			out.ReusableCallers++
		case hasTimeout:
			out.Covered++
		default:
			out.Violations = append(out.Violations,
				fmt.Sprintf("%s:%d job '%s' missing timeout-minutes", displayName, curLine, curJob))
		}
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if strings.TrimRight(line, " \t\r") == "jobs:" {
			inJobs = true
			continue
		}
		if !inJobs {
			continue
		}
		// A column-0 key ends the jobs section.
		if topLevelKeyRe.MatchString(line) {
			finalize()
			curJob = ""
			inJobs = false
			continue
		}
		if m := jobKeyRe.FindStringSubmatch(line); m != nil {
			finalize()
			curJob = m[1]
			curLine = lineNo
			hasTimeout = false
			isReusableCall = false
			continue
		}
		if curJob != "" {
			if jobTimeoutRe.MatchString(line) {
				hasTimeout = true
			}
			if reusableCallerRe.MatchString(line) {
				isReusableCall = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", displayName, err)
	}
	finalize()
	return nil
}
