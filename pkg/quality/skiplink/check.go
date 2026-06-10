// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package skiplink implements the skip-link enforcement gate (ADR T6.5/EC-8):
// every t.Skip()/it.skip()/xit() MUST have a nearby issue URL OR live in a
// `*_skip_test.go` / `*.skip.test.ts` file. Baseline-aware: violations listed
// in the baseline file are grandfathered; only NEW violations fail.
//
// Plan T5.3 of docs/plans/completed/2026-05-22-shell-script-strategic-plan.md v1.3:
// replaces infra/scripts/quality/check-skip-link.sh. Pure-Go (WalkDir + regexp)
// — no external CLI. Genuine pure-logic win per D5.
package skiplink

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultIssueURLPattern matches a GitHub issue URL.
const DefaultIssueURLPattern = `https://github\.com/[^/]+/[^/]+/issues/[0-9]+`

// contextRadius is the number of lines above/below a skip call searched for a URL.
const contextRadius = 3

// Config drives the gate.
type Config struct {
	TargetDir       string
	BaselineFile    string // path to baseline (empty + IgnoreBaseline=false → no baseline)
	IgnoreBaseline  bool   // true → all violations are "new" (tests/audit)
	IssueURLPattern string // default DefaultIssueURLPattern
}

// Outcome captures the result.
type Outcome struct {
	Pass          bool
	NewViolations []string
	BaselineCount int
}

var excludedDirs = map[string]bool{
	"vendor": true, "node_modules": true, ".git": true, "dist": true, "build": true,
	// `worktrees` — nested git worktrees (.claude/worktrees/<name>/) are
	// SEPARATE checkouts; scanning them double-counts and inflates violations.
	// The legacy bash gate lacked this exclusion (found 114 phantom violations
	// locally); CI never had worktrees so it passed. Excluding here makes
	// local == CI and is strictly more correct.
	"worktrees": true,
}

var (
	goSkipRe = regexp.MustCompile(`\b[tb]\.(Skip|SkipNow|Skipf)\(`)
	tsSkipRe = regexp.MustCompile(`(\b(it|describe|test)\.skip\(|^\s*x(it|describe)\()`)
)

type violation struct {
	key     string // relpath:line (baseline key)
	display string
}

// Check runs the gate.
func Check(cfg Config) (*Outcome, error) {
	urlPat := cfg.IssueURLPattern
	if urlPat == "" {
		urlPat = DefaultIssueURLPattern
	}
	urlRe, err := regexp.Compile(urlPat)
	if err != nil {
		return nil, fmt.Errorf("compile issue-url pattern: %w", err)
	}

	var violations []violation
	walkErr := filepath.WalkDir(cfg.TargetDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if excludedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if skipFileByName(name) {
			return nil
		}
		var re *regexp.Regexp
		switch {
		case strings.HasSuffix(name, "_test.go"):
			re = goSkipRe
		case strings.HasSuffix(name, ".test.ts"), strings.HasSuffix(name, ".spec.ts"),
			strings.HasSuffix(name, ".test.tsx"), strings.HasSuffix(name, ".spec.tsx"):
			re = tsSkipRe
		default:
			return nil
		}
		vs := scanFile(path, cfg.TargetDir, re, urlRe)
		violations = append(violations, vs...)
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk %s: %w", cfg.TargetDir, walkErr)
	}

	// Baseline subtraction.
	baseline := map[string]bool{}
	useBaseline := false
	if !cfg.IgnoreBaseline && cfg.BaselineFile != "" {
		if data, err := os.ReadFile(cfg.BaselineFile); err == nil {
			baseline = parseBaseline(string(data))
			useBaseline = true
		}
	}

	outcome := &Outcome{Pass: true}
	if useBaseline {
		outcome.BaselineCount = len(baseline)
	}
	for _, v := range violations {
		if useBaseline && baseline[v.key] {
			continue
		}
		outcome.Pass = false
		outcome.NewViolations = append(outcome.NewViolations, v.display)
	}
	return outcome, nil
}

// skipFileByName returns true for intentional-permanent-skip filenames.
func skipFileByName(name string) bool {
	for _, suffix := range []string{"_skip_test.go", ".skip.test.ts", ".skip.spec.ts"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// scanFile finds skip calls lacking an issue URL within ±contextRadius lines.
func scanFile(path, targetDir string, skipRe, urlRe *regexp.Regexp) []violation {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	rel, _ := filepath.Rel(targetDir, path)
	var out []violation
	for i, line := range lines {
		if !skipRe.MatchString(line) {
			continue
		}
		// Context window ±contextRadius.
		lo := i - contextRadius
		if lo < 0 {
			lo = 0
		}
		hi := i + contextRadius + 1
		if hi > len(lines) {
			hi = len(lines)
		}
		ctx := strings.Join(lines[lo:hi], "\n")
		if urlRe.MatchString(ctx) {
			continue
		}
		lineNum := i + 1
		out = append(out, violation{
			key:     fmt.Sprintf("%s:%d", rel, lineNum),
			display: fmt.Sprintf("%s:%d: %s", path, lineNum, strings.TrimSpace(line)),
		})
	}
	return out
}

// parseBaseline returns the set of baseline keys, stripping comments + blanks.
func parseBaseline(content string) map[string]bool {
	keys := map[string]bool{}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		keys[trimmed] = true
	}
	return keys
}
