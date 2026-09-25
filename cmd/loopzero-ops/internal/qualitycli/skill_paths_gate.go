// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"errors"
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/skillpaths"
)

func init() {
	register(Gate{
		Name:    "skill-paths",
		Summary: "T0.1 EC D5: every infra/scripts/ path AND every `task X` invocation cited in any .claude/skills/*/SKILL.md MUST resolve on disk. Ports the bash awk task-name extractor + flattened/namespaced include set walk into Go. Skips task-automation skill (generic third-party). Enforces dogfood no-GH_TOKEN/.env + to-plan no-`task teardown` anti-patterns. M-Migrate2 2026-07-07 replaces infra/tests/skills/skill-paths_test.sh.",
		Setup:   setupSkillPaths,
	})
}

// setupSkillPaths enforces T0.1 EC D5 — every infra/scripts/ path AND every
// `task <name>` invocation referenced in any .claude/skills/*/SKILL.md
// MUST resolve on disk (or be an allow-listed generic skill).
// Migrated from infra/tests/skills/skill-paths_test.sh
// (M-Migrate2 continuation 2026-07-07).
func setupSkillPaths(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := skillpaths.Check(skillpaths.Config{RepoRoot: inv.Root})
		if errors.Is(err, skillpaths.ErrSkillsRootMissing) {
			return kitNotInstalled(inv, err)
		}
		if err != nil {
			if !isViolation(err, skillpaths.ErrViolations) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "skill-paths: FAIL — %d broken reference(s) across SKILL.md files\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				fmt.Fprintf(inv.Stderr, "  %s\n", f)
			}
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "skill-paths: PASS — %d skills, %d paths, %d task invocations resolve\n",
			outcome.CheckedSkills, outcome.CheckedPaths, outcome.CheckedTasks)
		return Pass, nil
	}
}
