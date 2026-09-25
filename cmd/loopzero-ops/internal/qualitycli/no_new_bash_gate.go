// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/nonewbash"
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
	return func(inv Invocation) (Outcome, error) {
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
			return Fail, err
		}
		if !outcome.Pass {
			fmt.Fprintln(inv.Stderr, "no-new-bash: FAIL — new bash scripts detected outside allowlist:")
			for _, v := range outcome.Violations {
				fmt.Fprintf(inv.Stderr, "  - %s\n", v)
			}
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintf(inv.Stderr, "To fix: add the script to %s with justificativa per D4 criteria,\n", *allowlist)
			fmt.Fprintln(inv.Stderr, "OR migrate the function to: helmfile (D2), cmd/loopzero-ops/internal/quality/X Go gate (D5),")
			fmt.Fprintln(inv.Stderr, "Ginkgo (T4), go:generate (T5.2). See 2026-05-22-shell-script-strategic-plan")
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "no-new-bash: PASS — no new bash scripts outside allowlist")
		return Pass, nil
	}
}
