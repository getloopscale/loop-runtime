// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/lintcoverage"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "lint-coverage",
		Summary: "Every Go module is measured (lint + gofmt), with a per-module ratchet against regression.",
		Setup:   setupLintCoverage,
	})
}

func setupLintCoverage(fs *flag.FlagSet) Check {
	write := fs.Bool("write", false, "re-freeze the baseline with the measured values")
	return func(inv Invocation) (verdict.Result, error) {
		if *write {
			frozen, err := lintcoverage.Refreeze(lintcoverage.Config{RepoRoot: inv.Root})
			if err != nil {
				return nil, err
			}
			inv.Stdout.Printf("lint-coverage: baseline rewritten — %s\n", frozen.Reason)
			return frozen, nil
		}

		out, err := lintcoverage.Check(lintcoverage.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if out.Verdict() == verdict.Fail {
			inv.Stderr.Printf("lint-coverage: FAIL — %v\n", out.Violation)
			for _, r := range out.Regressions {
				inv.Stderr.Printf("  regression: %s [%s] %d → %d\n", r.Module, r.Dimension, r.Was, r.Now)
			}
			return out, nil
		}
		inv.Stdout.Printf("lint-coverage: PASS — %s\n", out.Reason)
		for _, i := range out.Improvements {
			inv.Stdout.Printf("  improved: %s %d → %d (re-freeze with `--write`)\n", i.Module, i.Was, i.Now)
		}
		return out, nil
	}
}
