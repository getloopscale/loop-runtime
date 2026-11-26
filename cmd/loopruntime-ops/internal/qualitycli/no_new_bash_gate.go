// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"
	"os"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/nonewbash"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "no-new-bash",
		Summary: "Blocks new .sh files not in the shell-scripts allowlist.",
		Setup:   setupNoNewBash,
	})
}

func setupNoNewBash(fs *flag.FlagSet) Check {
	allowlist := fs.String("allowlist", "infra/quality/shell-scripts-allowlist.md", "allowlist markdown file")
	baseRef := fs.String("base-ref", "origin/main", "base git ref")
	headRef := fs.String("head-ref", "HEAD", "head git ref")
	return func(inv Invocation) (verdict.Result, error) {
		absRoot, err := os.Getwd()
		if err == nil && inv.Root == "." {
			// Resolve "." to absolute path for clearer error messages
			inv.Root = absRoot
		}

		outcome, err := nonewbash.Check(nonewbash.Config{
			RepoRoot:      inv.Root,
			AllowlistPath: *allowlist,
			BaseRef:       *baseRef,
			HeadRef:       *headRef,
		})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Println("no-new-bash: FAIL — new bash scripts detected outside allowlist:")
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  - %s\n", v)
			}
			inv.Stderr.Println("")
			inv.Stderr.Printf("To fix: add the script to %s with a justification per D4 criteria,\n", *allowlist)
			inv.Stderr.Println("OR migrate the function to: helmfile (D2), cmd/loopruntime-ops/internal/quality/X Go gate (D5),")
			inv.Stderr.Println("Ginkgo (T4), go:generate (T5.2). See 2026-05-22-shell-script-strategic-plan")
			return outcome, nil
		}
		inv.Stdout.Println("no-new-bash: PASS — no new bash scripts outside allowlist")
		return outcome, nil
	}
}
