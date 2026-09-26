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
		if err != nil {
			return nil, err
		}
		switch {
		case out.Verdict() == verdict.Skip:
			inv.Stdout.Println("skill-suites: ADVISORY — python3/pytest absent; nothing was concluded")
		case out.Verdict() == verdict.NotVerified:
			reportKitNotInstalled(inv, out.Unverified)
		case errors.Is(out.Violation, skillsuites.ErrNoSuitesFound):
			inv.Stderr.Printf("skill-suites: FAIL — %v\n", out.Violation)
		case out.Verdict() == verdict.Fail:
			failed := out.Failed()
			inv.Stderr.Printf("skill-suites: FAIL — %d of %d suite(s) red:\n", len(failed), len(out.Results))
			for _, r := range failed {
				inv.Stderr.Printf("  %s\n      %s\n", r.Skill, r.Tail)
			}
		default:
			inv.Stdout.Printf("skill-suites: PASS — %d skill suite(s) green\n", len(out.Results))
		}
		return out, nil
	}
}
