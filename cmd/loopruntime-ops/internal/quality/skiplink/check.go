// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package skiplink implements the skip-link enforcement gate (ADR T6.5/EC-8):
// every t.Skip()/it.skip()/xit() MUST have a nearby issue URL OR live in a
// `*_skip_test.go` / `*.skip.test.ts` file. Baseline-aware: violations listed
// in the baseline file are grandfathered; only NEW violations fail.
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3:
// replaces infra/scripts/quality/check-skip-link.sh. Pure-Go (WalkDir + regexp)
// — no external CLI. Genuine pure-logic win per D5.
package skiplink

import (
	"fmt"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
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
	// Keys are the baseline keys of the reported violations, in the same order
	// as NewViolations. Regenerating the baseline is `Keys`, one per line — the
	// display strings carry absolute paths and cannot be fed back in.
	Keys []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

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
	// key identifies the skip by FILE + its own text, not by line number.
	//
	// It used to be relpath:LINE, which made the baseline positional: adding a
	// line anywhere above a grandfathered skip re-reported it as new. Measured
	// 2026-08-06 — the 145-entry baseline matched almost nothing and the gate
	// reported 189 "NEW" skips, most of them conditional environment skips it
	// had already accepted. A key that a refactor invalidates is a baseline
	// that decays into noise, and a noisy gate stops being read.
	key     string
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
		outcome.Keys = append(outcome.Keys, v.key)
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
// maskGoLiterals blanks the CONTENT of Go string and rune literals, preserving
// every byte offset and every newline, so a later line-based match sees code and
// not data.
//
// Why this exists: the Go detector below is a regexp over raw text, and a test
// that builds Go source as a fixture contains the literal bytes `t.Skip(`. On
// 2026-09-05 that reported ten violations in one file whose skips were all inside
// raw strings feeding an AST detector's fixtures -- data being read as code. The
// two escapes the gate offers were both wrong for it: the baseline is for
// grandfathered real skips, and renaming to *_skip_test.go would exempt every
// FUTURE real skip in a file of ordinary tests.
//
// It fails toward MORE detection, never less. A file the scanner cannot read
// cleanly is returned unchanged, so a malformed file is still matched in full: a
// masker that silently swallowed a real skip would be the worse failure, and this
// gate exists to find skips.
func maskGoLiterals(src []byte) []byte {
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, src, func(token.Position, string) {}, scanner.ScanComments)

	out := append([]byte(nil), src...)
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		// Comments are masked for the same reason literals are: `// t.Skip(...)`
		// is prose about a skip, not one. Safe for the URL check, which reads the
		// ORIGINAL lines, so an issue link in a comment beside a real skip is
		// still found.
		if tok != token.STRING && tok != token.CHAR && tok != token.COMMENT {
			continue
		}
		start := file.Offset(pos)
		end := start + len(lit)
		if start < 0 || end > len(out) {
			continue
		}
		for i := start; i < end; i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	if s.ErrorCount > 0 {
		return src
	}
	return out
}

func scanFile(path, targetDir string, skipRe, urlRe *regexp.Regexp) []violation {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	// Matched against masked source; reported and URL-checked against the
	// original, so a violation still shows the line a person wrote.
	matchOn := lines
	if strings.HasSuffix(path, ".go") {
		matchOn = strings.Split(string(maskGoLiterals(data)), "\n")
	}
	rel, _ := filepath.Rel(targetDir, path)
	var out []violation
	for i, line := range lines {
		if i >= len(matchOn) || !skipRe.MatchString(matchOn[i]) {
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
			key:     baselineKey(rel, line),
			display: fmt.Sprintf("%s:%d: %s", path, lineNum, strings.TrimSpace(line)),
		})
	}
	return out
}

// baselineKey identifies a skip by file plus the normalized text of the skip
// line, so the entry survives the line moving. Whitespace is collapsed because
// gofmt re-indents; the skip's own reason string still distinguishes it from a
// different skip in the same file.
func baselineKey(rel, line string) string {
	return rel + "\t" + strings.Join(strings.Fields(line), " ")
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
