// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/skillpaths"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
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
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := skillpaths.Check(skillpaths.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		switch outcome.Verdict() {
		case verdict.NotVerified:
			reportKitNotInstalled(inv, outcome.Unverified)
		case verdict.Fail:
			inv.Stderr.Printf("skill-paths: FAIL — %d broken reference(s) across SKILL.md files\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", f)
			}
		default:
			inv.Stdout.Printf("skill-paths: PASS — %d skills, %d paths, %d task invocations resolve\n",
				outcome.CheckedSkills, outcome.CheckedPaths, outcome.CheckedTasks)
		}
		return outcome, nil
	}
}
