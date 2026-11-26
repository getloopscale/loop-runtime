// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package mockbudget implements the mock-budget gate (ADR D8): max 5 mocks
// per test file (top-level declarations only). A `// mock-budget: N — reason`
// file comment raises the per-file budget.
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3:
// replaces infra/scripts/quality/check-mock-budget.sh. Pure-Go (filepath.WalkDir
// + regexp) — no external CLI. Genuine pure-logic win per D5.
package mockbudget

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// DefaultBudget is the per-file mock ceiling (ADR D8).
const DefaultBudget = 5

// Config drives the gate.
type Config struct {
	TargetDir string
	Budget    int
}

// Outcome captures the result.
type Outcome struct {
	Pass       bool
	Violations []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

var excludedDirs = map[string]bool{
	"vendor": true, "node_modules": true, ".git": true, "dist": true, "build": true,
}

var (
	// Go top-level mock declarations: `var x = mock.New...` OR gomock.NewController.
	goMockRe = regexp.MustCompile(`(?m)(^var\s+\w+\s*=\s*mock\.New|gomock\.NewController|^mock\w*\s*:?=\s*)`)
	// TS top-level jest.mock(/vi.mock( at line start (NOT inline jest.fn()).
	tsMockRe = regexp.MustCompile(`(?m)^\s*(jest|vi)\.mock\(`)
	// Opt-out comment: `// mock-budget: N`.
	optOutRe = regexp.MustCompile(`//\s*mock-budget:\s*([0-9]+)`)
)

// Check runs the gate.
func Check(cfg Config) (*Outcome, error) {
	// Budget is taken as-is from the caller. The CLI applies DefaultBudget when
	// the BUDGET env var is unset; tests pass an explicit budget (including 0
	// for strict mode).
	budget := cfg.Budget

	outcome := &Outcome{Pass: true}

	err := filepath.WalkDir(cfg.TargetDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if excludedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		count, optOut, ok, err := mockCountForFile(d.Name(), path)
		if err != nil {
			return err
		}
		if !ok {
			return nil // not a test file
		}
		fileBudget := budget
		if optOut >= 0 {
			fileBudget = optOut
		}
		if count > fileBudget {
			outcome.Pass = false
			rel, _ := filepath.Rel(cfg.TargetDir, path)
			outcome.Violations = append(outcome.Violations,
				fmt.Sprintf("%s: %d mocks (budget=%d)", rel, count, fileBudget))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", cfg.TargetDir, err)
	}
	return outcome, nil
}

// mockCountForFile reports, for a test file, its mock count and its opt-out
// budget (-1 when absent). ok is false for a file that is not a test file. A
// test file that cannot be read is an error: its budget cannot be vouched for.
func mockCountForFile(name, path string) (count, optOut int, ok bool, err error) {
	var counter func(string) int
	switch {
	case strings.HasSuffix(name, "_test.go"):
		counter = countGoMocks
	case strings.HasSuffix(name, ".test.ts"), strings.HasSuffix(name, ".spec.ts"),
		strings.HasSuffix(name, ".test.tsx"), strings.HasSuffix(name, ".spec.tsx"):
		counter = countTSMocks
	default:
		return 0, -1, false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, -1, false, fmt.Errorf("reading test file: %w", err)
	}
	src := string(data)
	return counter(src), readOptOutBudget(src), true, nil
}

func countGoMocks(src string) int {
	return len(goMockRe.FindAllString(src, -1))
}

func countTSMocks(src string) int {
	return len(tsMockRe.FindAllString(src, -1))
}

// readOptOutBudget parses `// mock-budget: N` from the first 50 lines.
// Returns -1 if absent.
func readOptOutBudget(src string) int {
	lines := strings.SplitN(src, "\n", 51)
	limit := len(lines)
	if limit > 50 {
		limit = 50
	}
	for _, line := range lines[:limit] {
		if m := optOutRe.FindStringSubmatch(line); m != nil {
			n, err := strconv.Atoi(m[1])
			if err == nil {
				return n
			}
		}
	}
	return -1
}
