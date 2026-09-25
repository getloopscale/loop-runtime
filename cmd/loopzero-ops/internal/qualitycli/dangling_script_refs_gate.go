// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/danglingrefs"
)

func init() {
	register(Gate{
		Name:    "dangling-script-refs",
		Summary: "Detects shell-script references that no longer resolve after a change.",
		Setup:   setupDanglingScriptRefs,
	})
}

// setupDanglingScriptRefs is the T0.1 backstop gate: fail if a deleted .sh is
// still referenced (live) by any caller surface. Requires fetch-depth: 0.
func setupDanglingScriptRefs(fs *flag.FlagSet) Check {
	baseRef := fs.String("base-ref", "origin/develop", "base git ref")
	headRef := fs.String("head-ref", "HEAD", "head git ref")
	return func(inv Invocation) (Outcome, error) {
		outcome, err := danglingrefs.Check(danglingrefs.Config{
			RepoRoot: inv.Root,
			BaseRef:  *baseRef,
			HeadRef:  *headRef,
		})
		if err != nil {
			return Fail, err
		}
		if !outcome.Pass {
			inv.Stderr.Println("dangling-script-refs: FAIL — deleted scripts still referenced by live callers:")
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  - %s\n", v)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("Per plan D2: rewire ALL 4 caller surfaces (Taskfile/CI/SKILL/scripts) in the")
			inv.Stderr.Println("same PR that deletes the script. See 2026-05-23-incremental-bash-to-go-migration-plan")
			return Fail, nil
		}
		inv.Stdout.Println("dangling-script-refs: PASS — no deleted script left with live references")
		return Pass, nil
	}
}
