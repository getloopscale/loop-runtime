// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package skillscriptpaths refuses a skill instruction that tells an agent to run a
// script by a path the agent cannot reach.
//
// # The finding
//
// The ecosystem lives at `<project>/.claude/` in a plugin install and at `<repo>/` in
// the standalone kit. A SKILL.md that says
//
//	python3 skills/release/scripts/compute_next_version.py
//
// resolves in exactly one of the two, and the agent following it in the other gets
// "No such file or directory". Measured 2026-08-16 (B-120): **19** such invocations
// across `auto-plan` (6), `release` (5), `implement` (5), `cycle-goal` (3), `acceptance`
// (3) and `arch-check` (1) — four of them in skills on the critical path of every
// delivery. Ten other invocations in the same corpus already carried `.claude/`, which
// is how the split is known to be drift and not a decision: somebody fixed ten and the
// rest stayed.
//
// # Invocations, not references
//
// The gate targets a path preceded by an INTERPRETER (`python3`, `bash`, `sh`). A path
// merely NAMED in prose is a different thing and is left alone:
//
//   - `skills/auto-plan/scripts/select_next_milestone.py` — Step 0 milestone selector
//
// That line sits in a list beside `rules/cycle-review.md` and `commands/plan-goal.md`,
// which use the same ecosystem-root-relative convention. A reader resolves it; nobody
// executes it. Widening the gate to catch references would make it demand `$ECO` inside
// prose that is not shell — a rule that produces a worse document to satisfy a checker.
//
// # The accepted form
//
// Whatever resolves in BOTH layouts. The idiom the repository already uses, mirroring
// `hooks/lib/detect-layout.sh`:
//
//	ECO=$([ -d .claude/skills ] && echo .claude || echo .)
//	python3 "$ECO/skills/release/scripts/compute_next_version.py"
//
// An explicit `.claude/skills/...` is ALSO accepted: it is correct wherever the plugin
// layout is what ships, and refusing it would break the ten invocations that already
// carry it. What is refused is the bare form, which is correct in at most one layout and
// silent in the other.
package skillscriptpaths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ErrUnreachableInvocation means a SKILL.md tells an agent to run a script by a path
// that resolves in only one of the two ecosystem layouts.
var ErrUnreachableInvocation = errors.New("a skill invokes a script by a path that does not resolve in the layout it runs in — the agent following it gets 'No such file or directory', and which layout it breaks in depends on how the ecosystem was installed")

// ErrSkillsRootMissing means the skills directory is absent, so nothing was checked.
// Absence is a FAILURE and not a skip: a gate that reports green over a tree it never
// found is the shape of defect this whole gate exists to refuse.
var ErrSkillsRootMissing = errors.New("the skills root is absent — nothing was scanned, and a green verdict over an unscanned tree is worthless")

// invocationRE matches an interpreter followed by a bare `skills/...` path.
//
// The negative lookahead on `"` and the absence of `.claude/` or `$ECO/` before
// `skills/` are what separate the broken form from the two accepted ones. Go's regexp
// has no lookahead, so the prefix check is done in code — see scanLine.
var invocationRE = regexp.MustCompile(`\b(?:python3|python|bash|sh)\s+("?)([A-Za-z0-9_./$-]*skills/[a-z0-9-]+/(?:scripts|tests)/[A-Za-z0-9_.-]+\.(?:py|sh))`)

// Finding is one unreachable invocation.
type Finding struct {
	File string
	Line int
	Path string
	Text string
}

// Outcome reports what the scan saw.
type Outcome struct {
	SkillsScanned int
	FilesScanned  int
	Findings      []Finding
}

// Config selects the tree to scan.
type Config struct {
	RepoRoot string
	// SkillsRel is where the skills live relative to RepoRoot. Defaults to the plugin
	// layout, which is what a consumer repository has.
	SkillsRel string
}

// accepted reports whether a matched path is reachable in both layouts.
//
// Two forms pass: an explicit ecosystem prefix (`.claude/skills/...`), correct wherever
// the plugin layout ships; and a shell-resolved prefix (`$ECO/skills/...`), correct in
// both. Everything else is the bare form.
func accepted(path string) bool {
	return strings.Contains(path, ".claude/skills/") || strings.Contains(path, "$ECO/")
}

func scanLine(line string) []string {
	var bad []string
	for _, m := range invocationRE.FindAllStringSubmatch(line, -1) {
		if !accepted(m[2]) {
			bad = append(bad, m[2])
		}
	}
	return bad
}

// Check is the gate entry point.
func Check(cfg Config) (Outcome, error) {
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	if cfg.SkillsRel == "" {
		cfg.SkillsRel = filepath.Join(".claude", "skills")
	}
	root := filepath.Join(cfg.RepoRoot, cfg.SkillsRel)

	entries, err := os.ReadDir(root)
	if err != nil {
		return Outcome{}, fmt.Errorf("%w: %s", ErrSkillsRootMissing, root)
	}

	var out Outcome
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out.SkillsScanned++
		skillDoc := filepath.Join(root, e.Name(), "SKILL.md")
		body, readErr := os.ReadFile(skillDoc)
		if readErr != nil {
			continue // a skill without a SKILL.md has no instruction to check
		}
		out.FilesScanned++
		rel := filepath.Join(cfg.SkillsRel, e.Name(), "SKILL.md")
		for i, line := range strings.Split(string(body), "\n") {
			for _, p := range scanLine(line) {
				out.Findings = append(out.Findings, Finding{
					File: rel, Line: i + 1, Path: p, Text: strings.TrimSpace(line),
				})
			}
		}
	}

	sort.Slice(out.Findings, func(i, j int) bool {
		if out.Findings[i].File != out.Findings[j].File {
			return out.Findings[i].File < out.Findings[j].File
		}
		return out.Findings[i].Line < out.Findings[j].Line
	})

	if len(out.Findings) > 0 {
		return out, ErrUnreachableInvocation
	}
	return out, nil
}
