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
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// ErrUnreachableInvocation means a SKILL.md tells an agent to run a script by a path
// that resolves in only one of the two ecosystem layouts.
var ErrUnreachableInvocation = errors.New("a skill invokes a script by a path that does not resolve in the layout it runs in — the agent following it gets 'No such file or directory', and which layout it breaks in depends on how the ecosystem was installed")

// ErrSkillsRootMissing means the skills directory is absent, so nothing was checked.
// The verdict is Skip with this reason printed, never a silent pass: the kit is a
// gitignored dependency, and a clone without it has nothing of this repository's
// own to verify.
var ErrSkillsRootMissing = errors.New("the skills root is absent — it is the installed, gitignored kit, so nothing was verified on this machine")

// invocationRE matches an interpreter followed by a bare `skills/...` path.
//
// The negative lookahead on `"` and the absence of `.claude/` or `$ECO/` before
// `skills/` are what separate the broken form from the two accepted ones. Go's regexp
// has no lookahead, so the prefix check is done in code — see scanLine.
// The alternation covers BOTH shapes the kit ships: `skills/<name>/scripts/…` and a bare
// `scripts/…` at the ecosystem root. Until 2026-08-29 it matched only the first, so a
// `python3 scripts/check_xrefs.py` in rules/README.md was invisible — the kit's own sweep
// found 39 occurrences where this gate saw 25.
var invocationRE = regexp.MustCompile(`\b(?:python3|python|bash|sh)\s+("?)([A-Za-z0-9_./$-]*(?:skills/[a-z0-9-]+/(?:scripts|tests)|scripts)/[A-Za-z0-9_.-]+\.(?:py|sh))`)

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
	// Violation is what the check found wrong (wrapping one of its Err*
	// reasons), or nil. A violation is the check's verdict, never its error.
	Violation error
	// Unverified is why the check could verify nothing on this machine
	// (wrapping one of its Err* reasons), or nil.
	Unverified error
}

// Verdict is Fail when the check found a violation, Skip when the kit is not
// installed (a gitignored dependency, absent from every fresh clone — owner
// decision, 2026-09-26), NotVerified when anything else left it unable to
// verify, and Pass otherwise.
func (o Outcome) Verdict() verdict.Verdict {
	switch {
	case o.Violation != nil:
		return verdict.Fail
	case errors.Is(o.Unverified, ErrSkillsRootMissing):
		return verdict.Skip
	case o.Unverified != nil:
		return verdict.NotVerified
	}
	return verdict.Pass
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
// accepted decides whether an invocation resolves in BOTH layouts.
//
// It keys on `.claude/` and on the runtime-resolved prefix, not on `.claude/skills/`: once the
// regex learned bare `scripts/…`, keying on the skills segment rejected `.claude/scripts/…`,
// which is correct wherever the plugin layout holds. The counterpart test caught that within
// the same change — widening WHAT is matched must not narrow WHAT is accepted.
func accepted(path string) bool {
	// Any prefix resolved at RUNTIME is fine — the shell decides the layout, not the text.
	// `$SKILL_DIR` is the kit's own variable for the skill's own directory; it was flagged as
	// soon as the regex learned bare `scripts/…`, which is a false positive: those two lines
	// already work in both layouts.
	if strings.HasPrefix(strings.TrimPrefix(path, `"`), "$") {
		return true
	}
	return strings.Contains(path, ".claude/") || strings.Contains(path, "$") ||
		strings.Contains(path, "echo .claude")
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
	if errors.Is(err, fs.ErrNotExist) {
		return Outcome{Unverified: fmt.Errorf("%w: %s", ErrSkillsRootMissing, root)}, nil
	}
	if err != nil {
		return Outcome{}, fmt.Errorf("reading %s: %w", root, err)
	}

	var out Outcome
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out.SkillsScanned++
		skillDoc := filepath.Join(root, e.Name(), "SKILL.md")
		body, readErr := os.ReadFile(skillDoc)
		if errors.Is(readErr, fs.ErrNotExist) {
			continue // a skill without a SKILL.md has no instruction to check
		}
		if readErr != nil {
			return out, fmt.Errorf("reading %s: %w", skillDoc, readErr)
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

	// The second blind spot: instruction for an agent is not confined to SKILL.md. `rules/`,
	// `commands/` and the ecosystem README carry the same invocations, and scanning only
	// SKILL.md reported PASS over a subset — every file it opened WAS clean.
	//
	// Measured 2026-08-29: the kit's own sweep found 39 occurrences in 18 files where this
	// gate saw 25 in 8. The difference was these directories plus the bare `scripts/…` form.
	eco := filepath.Dir(filepath.Join(cfg.RepoRoot, cfg.SkillsRel))
	for _, sub := range []string{"rules", "commands", "."} {
		dir := filepath.Join(eco, sub)
		items, derr := os.ReadDir(dir)
		if errors.Is(derr, fs.ErrNotExist) {
			continue // an ecosystem without rules/ or commands/ has nothing there to check
		}
		if derr != nil {
			return out, fmt.Errorf("reading %s: %w", dir, derr)
		}
		for _, it := range items {
			if it.IsDir() || !strings.HasSuffix(it.Name(), ".md") {
				continue
			}
			// `*.local.md` is session state, not shipped instruction: gitignored, present only
			// on the machine that ran the loop. Failing the repository over a file no consumer
			// receives is the gate reporting on the wrong thing. The `.claude/` root carries one
			// today with 16 invocations in it.
			if strings.HasSuffix(it.Name(), ".local.md") {
				continue
			}
			doc := filepath.Join(dir, it.Name())
			body, rerr := os.ReadFile(doc) // #nosec G304 -- path built from the ecosystem root
			if rerr != nil {
				return out, fmt.Errorf("reading %s: %w", doc, rerr)
			}
			out.FilesScanned++
			rel, relErr := filepath.Rel(cfg.RepoRoot, doc)
			if relErr != nil {
				return out, fmt.Errorf("locating %s under %s: %w", doc, cfg.RepoRoot, relErr)
			}
			for i, line := range strings.Split(string(body), "\n") {
				for _, pth := range scanLine(line) {
					out.Findings = append(out.Findings, Finding{
						File: filepath.ToSlash(rel), Line: i + 1, Path: pth,
						Text: strings.TrimSpace(line),
					})
				}
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
		out.Violation = ErrUnreachableInvocation
		return out, nil
	}
	return out, nil
}
