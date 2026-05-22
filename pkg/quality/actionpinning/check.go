// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package actionpinning implements the INF-G18 gate: enforce SHA-pinning of
// GitHub Actions in .github/workflows/*.yml.
//
// Plan T5.3 of docs/plans/2026-05-22-shell-script-strategic-plan.md v1.3:
// replaces scripts/check-action-pinning.sh. Pure-Go (filepath walk + regexp +
// string compare) — no external CLI. Per D5: a MIGRATE-GO-PUREFN gate.
//
// Every `uses:` reference must pin to a 40-char commit SHA, never a mutable
// tag. The tj-actions/changed-files supply-chain attack (March 2025) showed
// tag-pinning is exploitable.
//
// Exemptions:
//  1. Internal reusable workflows: `uses: ./.github/...` (relative path).
//  2. Explicit `# pragma: allow-tag-pin` within the comment-only lines
//     immediately preceding the `uses:` line (PRAGMA_LOOKBACK window).
package actionpinning

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// PragmaLookback is how many prior comment-only lines may carry the pragma.
const PragmaLookback = 4

// Config drives the gate.
type Config struct {
	// WorkflowsDir is the directory holding *.yml workflow files.
	WorkflowsDir string
}

// Outcome captures the result.
type Outcome struct {
	Pass          bool
	WorkflowCount int
	Violations    []string // "file:line: <line>"
}

var (
	// Matches both inline list form `- uses: foo@ref` and standalone `uses: foo@ref`.
	// The legacy bash gate (scripts/check-action-pinning.sh) used `^[[:space:]]*uses:`
	// which silently MISSED the inline `- uses:` form — a latent no-op bug, since
	// 100% of LoopZero workflows use the inline form (see finding-2026-05-22-action-pinning-noop).
	usesLineRe = regexp.MustCompile(`^[[:space:]]*-?[[:space:]]*uses:[[:space:]]+([^[:space:]]+)`)
	sha40Re    = regexp.MustCompile(`^[0-9a-f]{40}$`)
	// A comment-only or blank line (pragma association continues across these).
	commentOrBlankRe = regexp.MustCompile(`^[[:space:]]*(#|$)`)
	pragmaRe         = regexp.MustCompile(`^[[:space:]]*#[[:space:]]*pragma:[[:space:]]*allow-tag-pin`)
)

// Check runs the gate against all *.yml files in cfg.WorkflowsDir.
func Check(cfg Config) (*Outcome, error) {
	info, err := os.Stat(cfg.WorkflowsDir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("workflows dir %s not found", cfg.WorkflowsDir)
	}

	entries, err := os.ReadDir(cfg.WorkflowsDir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", cfg.WorkflowsDir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".yml") {
			files = append(files, filepath.Join(cfg.WorkflowsDir, e.Name()))
		}
	}
	sort.Strings(files)

	outcome := &Outcome{Pass: true, WorkflowCount: len(files)}
	for _, f := range files {
		v, err := scanFile(f)
		if err != nil {
			return nil, err
		}
		outcome.Violations = append(outcome.Violations, v...)
	}
	if len(outcome.Violations) > 0 {
		outcome.Pass = false
	}
	return outcome, nil
}

// scanFile returns the list of "file:line: <line>" violations in one workflow.
func scanFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var violations []string
	var prev []string // sliding window of prior lines (most recent last)
	lineNum := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		lineNum++

		ref, ok := matchUses(line)
		if !ok {
			prev = pushWindow(prev, line)
			continue
		}

		if !isCompliant(ref) && !pragmaPrecedes(prev) {
			violations = append(violations, fmt.Sprintf("%s:%d: %s", path, lineNum, line))
		}
		prev = pushWindow(prev, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return violations, nil
}

// matchUses returns the ref token of a `uses:` line, or ok=false.
func matchUses(line string) (string, bool) {
	m := usesLineRe.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// isCompliant reports whether a `uses:` ref is exempt or SHA-pinned.
func isCompliant(ref string) bool {
	// Internal reusable workflows (relative paths) don't traverse a registry.
	if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, ".github/") {
		return true
	}
	// No @ — not a registry action ref (defensive).
	at := strings.LastIndex(ref, "@")
	if at < 0 {
		return true
	}
	token := ref[at+1:]
	return sha40Re.MatchString(token)
}

// pragmaPrecedes reports whether a `# pragma: allow-tag-pin` appears in the
// contiguous comment-only block immediately above the current line. A
// non-comment, non-blank line breaks the association.
func pragmaPrecedes(prev []string) bool {
	for i := len(prev) - 1; i >= 0; i-- {
		pl := prev[i]
		if !commentOrBlankRe.MatchString(pl) {
			return false
		}
		if pragmaRe.MatchString(pl) {
			return true
		}
	}
	return false
}

// pushWindow appends line and trims the window to PragmaLookback entries.
func pushWindow(prev []string, line string) []string {
	prev = append(prev, line)
	if len(prev) > PragmaLookback {
		prev = prev[len(prev)-PragmaLookback:]
	}
	return prev
}
