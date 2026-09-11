// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package decisionsdeletion implements the T1.14 gate: fail CI if a protected
// governance artifact (ADR, MEET DR, operations runbook/inventory) is deleted
// in a PR without a sibling file carrying a `superseded_by:` reference.
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3:
// replaces infra/scripts/check-decisions-deletion.sh + its hermetic test
// (D6 AUTO-DIE). Pure-Go decision logic over `git` output (git is on the D5
// allow-list — only pulumi/helm/kubectl/aws/gh are forbidden shell-wraps).
//
// Per .claude/rules/pre-release-governance.md § Plan Amendment Policy, deletion
// of a ratified artifact requires a superseding file with
// `superseded_by: <deleted-basename>` frontmatter (or explicit MEET ratification).
package decisionsdeletion

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// protectedPath couples a directory with the glob of protected basenames.
type protectedPath struct {
	Dir  string
	Glob string
}

// ProtectedPaths are the governance artifact locations under deletion guard.
var ProtectedPaths = []protectedPath{
	{"docs/wiki/decisions", "*.md"},
	{"docs/wiki/meetings", "*.md"},
	{"docs/wiki/concepts/operations", "*-runbook.md"},
	{"docs/wiki/concepts/operations", "*-inventory.md"},
}

// Config drives the gate.
type Config struct {
	RepoRoot string // dir to run git in (default ".")
	BaseRef  string
	HeadRef  string
}

// Outcome captures the result.
type Outcome struct {
	Pass         bool
	DeletedFiles []string // protected files deleted in the diff
	Superseded   []string // "deleted -> superseder" for properly-superseded ones
	Violations   []string // deleted without a superseder
}

// Check runs the gate. Git invocation failures (unknown ref/path, non-repo) are
// tolerated as "no data" — mirroring the bash gate's `|| true`; a missing diff
// means nothing was deleted, which is a clean result.
func Check(cfg Config) *Outcome {
	root := cfg.RepoRoot
	if root == "" {
		root = "."
	}
	base, head := cfg.BaseRef, cfg.HeadRef
	if base == "" {
		base = "origin/main"
	}
	if head == "" {
		head = "HEAD"
	}

	out := &Outcome{Pass: true}

	// 1. Collect protected files deleted between base and head.
	seen := map[string]bool{}
	for _, p := range ProtectedPaths {
		for _, f := range gitDeletedInDir(root, base, head, p.Dir) {
			if ok, _ := filepath.Match(p.Glob, filepath.Base(f)); ok && !seen[f] {
				seen[f] = true
				out.DeletedFiles = append(out.DeletedFiles, f)
			}
		}
	}
	sort.Strings(out.DeletedFiles)
	if len(out.DeletedFiles) == 0 {
		return out
	}

	// 2. For each deleted file, look for a sibling superseder at HEAD.
	for _, deleted := range out.DeletedFiles {
		if superseder := findSuperseder(root, head, filepath.Dir(deleted), filepath.Base(deleted)); superseder != "" {
			out.Superseded = append(out.Superseded, fmt.Sprintf("%s -> %s", deleted, superseder))
		} else {
			out.Violations = append(out.Violations, deleted)
		}
	}
	if len(out.Violations) > 0 {
		out.Pass = false
	}
	return out
}

// gitDeletedInDir lists files deleted (status D) under dir between base..head.
func gitDeletedInDir(root, base, head, dir string) []string {
	return gitLines(root, []string{"diff", "--name-only", "--diff-filter=D",
		fmt.Sprintf("%s...%s", base, head), "--", dir + "/"})
}

// findSuperseder returns the first .md file at head under dir whose content
// references deletedBase via `superseded_by:` or a YAML list item, or "" if none.
func findSuperseder(root, head, dir, deletedBase string) string {
	q := regexp.QuoteMeta(deletedBase)
	re := regexp.MustCompile(`(?m)^superseded_by:.*` + q + `|^\s+- ` + q)
	for _, cand := range gitLines(root, []string{"ls-tree", "-r", head, "--name-only", "--", dir + "/"}) {
		if !strings.HasSuffix(cand, ".md") {
			continue
		}
		if re.MatchString(gitShow(root, head, cand)) {
			return cand
		}
	}
	return ""
}

// gitLines runs git under root and returns non-empty output lines. Git failures
// (unknown ref/path, non-repo) are tolerated as "no lines" (bash `|| true`).
func gitLines(root string, args []string) []string {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	outBytes, err := cmd.Output()
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(string(outBytes), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// gitShow returns the content of path at ref, or "" if unreadable.
func gitShow(root, ref, path string) string {
	cmd := exec.Command("git", "show", ref+":"+path)
	cmd.Dir = root
	b, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(b)
}
