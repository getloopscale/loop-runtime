// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"errors"
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/skillscriptpaths"
)

func init() {
	register(Gate{
		Name:    "skill-script-paths",
		Summary: "Every script invocation in a SKILL.md resolves in BOTH ecosystem layouts (.claude/ in the plugin, the root in the standalone kit). A bare path breaks in one of them, and which one depends on how the ecosystem was installed (B-120).",
		Setup:   setupSkillScriptPaths,
	})
}

func setupSkillScriptPaths(fs *flag.FlagSet) Check {
	skills := fs.String("skills", "", "skills dir relative to root (default: .claude/skills)")
	return func(inv Invocation) (Outcome, error) {
		out, err := skillscriptpaths.Check(skillscriptpaths.Config{RepoRoot: inv.Root, SkillsRel: *skills})
		switch {
		case errors.Is(err, skillscriptpaths.ErrSkillsRootMissing):
			return kitNotInstalled(inv, err)
		case errors.Is(err, skillscriptpaths.ErrUnreachableInvocation):
			fmt.Fprintf(inv.Stderr, "skill-script-paths: FAIL — %d invocation(s) resolve in only one layout:\n", len(out.Findings))
			for _, f := range out.Findings {
				fmt.Fprintf(inv.Stderr, "  %s:%d  %s\n      %s\n", f.File, f.Line, f.Path, f.Text)
			}
			fmt.Fprintln(inv.Stderr, "\nUse a prefix that resolves in both:\n"+
				"  ECO=$([ -d .claude/skills ] && echo .claude || echo .)\n"+
				"  python3 \"$ECO/skills/<skill>/scripts/<script>.py\"")
			return Fail, nil
		case err != nil:
			fmt.Fprintf(inv.Stderr, "skill-script-paths: FAIL — %v\n", err)
			return Fail, nil
		}
		// "document(s)", not "SKILL.md": the scan reaches rules/, commands/ and the ecosystem root
		// too, so the count stopped being SKILL.md the moment it was widened. It read 98 where 49
		// SKILL.md exist on disk — the number was right and the noun was not, which reads as
		// double-counting to anyone checking.
		fmt.Fprintf(inv.Stdout, "skill-script-paths: PASS — %d document(s) across %d skill(s) plus rules/, commands/ and the ecosystem root; every script invocation resolves in both layouts\n",
			out.FilesScanned, out.SkillsScanned)
		return Pass, nil
	}
}
