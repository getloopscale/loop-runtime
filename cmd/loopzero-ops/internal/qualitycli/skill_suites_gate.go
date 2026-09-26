// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"errors"
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/skillsuites"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "skill-suites",
		Summary: "Runs the pytest suites under .claude/skills/*/tests/, which no task ran. Advisory when pytest is absent; FAILS when discovery finds no suite, because finding nothing is not the same as finding nothing wrong; NOT VERIFIED (exit 5) where the gitignored kit is not installed (B-124).",
		Setup:   setupSkillSuites,
	})
}

// runSkillScriptPaths refuses a SKILL.md that tells an agent to run a script by a path
// which resolves in only one of the two ecosystem layouts (`<project>/.claude/` for a
// plugin install, `<repo>/` for the standalone kit). B-120.
// runSkillSuites runs the ecosystem's own pytest suites, which no task ran before B-124 —
// three of fifteen were red, and the defect two of them caught had already shipped.
func setupSkillSuites(fs *flag.FlagSet) Check {
	skills := fs.String("skills", "", "skills dir relative to root (default: .claude/skills)")
	return func(inv Invocation) (verdict.Result, error) {
		out, err := skillsuites.Check(skillsuites.Config{RepoRoot: inv.Root, SkillsRel: *skills})
		switch {
		case out.Advisory:
			inv.Stdout.Println("skill-suites: ADVISORY — python3/pytest absent; nothing was concluded")
			return Pass, nil
		case errors.Is(err, skillsuites.ErrSkillsRootMissing):
			return kitNotInstalled(inv, err)
		case errors.Is(err, skillsuites.ErrNoSuitesFound):
			inv.Stderr.Printf("skill-suites: FAIL — %v\n", err)
			return Fail, nil
		case errors.Is(err, skillsuites.ErrSuitesFailed):
			failed := out.Failed()
			inv.Stderr.Printf("skill-suites: FAIL — %d of %d suite(s) red:\n", len(failed), len(out.Results))
			for _, r := range failed {
				inv.Stderr.Printf("  %s\n      %s\n", r.Skill, r.Tail)
			}
			return Fail, nil
		case err != nil:
			inv.Stderr.Printf("skill-suites: FAIL — %v\n", err)
			return Fail, nil
		}
		inv.Stdout.Printf("skill-suites: PASS — %d skill suite(s) green\n", len(out.Results))
		return Pass, nil
	}
}
