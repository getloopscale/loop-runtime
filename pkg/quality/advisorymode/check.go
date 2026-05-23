// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package advisorymode is the Go home for the advisory-mode-annotated gate
// (migrated from infra/tests/workflows/advisory-mode-annotated_test.sh, plan
// 2026-05-23, originally T6.8 + ADR D4).
//
// Every `continue-on-error: true` in a workflow MUST carry, within the 5 lines
// above (through 1 line below), either a rationale comment OR a graduation
// marker — otherwise advisory mode is honor-system that lasts forever.
package advisorymode

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
	WorkflowsDir string
}

// Outcome is the gate result.
type Outcome struct {
	Pass       bool
	Total      int
	Annotated  int
	Violations []string
}

// continueOnErrorRe matches an advisory-mode declaration.
var continueOnErrorRe = regexp.MustCompile(`continue-on-error:\s*true`)

// annotatedKeys are the lowercase rationale/graduation markers (matched
// case-insensitively as substrings), mirroring the original bash list.
var annotatedKeys = []string{
	"graduation:", "rationale:", "rationale (", "required so", "post-task",
	"best-effort", "advisory", "continue on", "auto-issue", "evidence", "fork pr",
}

// Check scans every workflow for un-annotated `continue-on-error: true`.
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

func scanWorkflow(absPath, displayName string, out *Outcome) error {
	// #nosec G304 -- absPath comes from os.ReadDir of the configured dir.
	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", displayName, err)
	}
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", displayName, err)
	}

	for i, line := range lines {
		if !continueOnErrorRe.MatchString(line) {
			continue
		}
		lineNo := i + 1
		out.Total++
		if isAnnotated(lines, lineNo) {
			out.Annotated++
		} else {
			out.Violations = append(out.Violations,
				fmt.Sprintf("%s:%d continue-on-error: true without rationale comment OR graduation marker", displayName, lineNo))
		}
	}
	return nil
}

// isAnnotated reports whether any annotated key appears in the window of 5 lines
// above through 1 line below (1-based lineNo), matching the bash gate.
func isAnnotated(lines []string, lineNo int) bool {
	start := lineNo - 5
	if start < 1 {
		start = 1
	}
	end := lineNo + 1
	if end > len(lines) {
		end = len(lines)
	}
	window := strings.ToLower(strings.Join(lines[start-1:end], "\n"))
	for _, k := range annotatedKeys {
		if strings.Contains(window, k) {
			return true
		}
	}
	return false
}
