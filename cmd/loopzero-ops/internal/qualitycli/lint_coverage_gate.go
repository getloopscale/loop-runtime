// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"errors"
	"flag"

	"path/filepath"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/lintcoverage"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
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
			out, merr := lintcoverage.Measure(lintcoverage.Config{RepoRoot: inv.Root})
			// Re-freezing uses what was MEASURED, even after a regression — it is a
			// deliberate act, and the file's diff is where it becomes visible.
			if merr != nil {
				return Fail, merr
			}
			if len(out.Lint) == 0 && len(out.Gofmt) == 0 {
				return Fail, errors.New("nothing measured, so nothing to freeze")
			}
			p := filepath.Join(inv.Root, lintcoverage.BaselineRelPath)
			if werr := lintcoverage.WriteBaseline(p, out.Lint, out.Gofmt); werr != nil {
				return Fail, werr
			}
			inv.Stdout.Printf("lint-coverage: baseline rewritten — %s\n", out.Reason)
			return Pass, nil
		}

		out, err := lintcoverage.Check(lintcoverage.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, lintcoverage.ErrRegression, lintcoverage.ErrUndeclaredModule) {
				return Fail, err
			}
			inv.Stderr.Printf("lint-coverage: FAIL — %v\n", err)
			for _, r := range out.Regressions {
				inv.Stderr.Printf("  regression: %s [%s] %d → %d\n", r.Module, r.Dimension, r.Was, r.Now)
			}
			return Fail, nil
		}
		inv.Stdout.Printf("lint-coverage: PASS — %s\n", out.Reason)
		for _, i := range out.Improvements {
			inv.Stdout.Printf("  improved: %s %d → %d (re-freeze with `--write`)\n", i.Module, i.Was, i.Now)
		}
		return Pass, nil
	}
}
