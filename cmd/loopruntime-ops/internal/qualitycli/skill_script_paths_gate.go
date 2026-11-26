// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/skillscriptpaths"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
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
	return func(inv Invocation) (verdict.Result, error) {
		out, err := skillscriptpaths.Check(skillscriptpaths.Config{RepoRoot: inv.Root, SkillsRel: *skills})
		if err != nil {
			return nil, err
		}
		switch out.Verdict() {
		case verdict.Skip:
			reportKitNotInstalled(inv, out.Unverified)
			return out, nil
		case verdict.NotVerified:
			inv.Stderr.Printf("skill-script-paths: NOT VERIFIED — %v\n", out.Unverified)
			return out, nil
		case verdict.Fail:
			inv.Stderr.Printf("skill-script-paths: FAIL — %d invocation(s) resolve in only one layout:\n", len(out.Findings))
			for _, f := range out.Findings {
				inv.Stderr.Printf("  %s:%d  %s\n      %s\n", f.File, f.Line, f.Path, f.Text)
			}
			inv.Stderr.Println("\nUse a prefix that resolves in both:\n" +
				"  ECO=$([ -d .claude/skills ] && echo .claude || echo .)\n" +
				"  python3 \"$ECO/skills/<skill>/scripts/<script>.py\"")
			return out, nil
		}
		// "document(s)", not "SKILL.md": the scan reaches rules/, commands/ and the ecosystem root
		// too, so the count stopped being SKILL.md the moment it was widened. It read 98 where 49
		// SKILL.md exist on disk — the number was right and the noun was not, which reads as
		// double-counting to anyone checking.
		inv.Stdout.Printf("skill-script-paths: PASS — %d document(s) across %d skill(s) plus rules/, commands/ and the ecosystem root; every script invocation resolves in both layouts\n",
			out.FilesScanned, out.SkillsScanned)
		return out, nil
	}
}
