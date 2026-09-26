// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/scriptcount"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "script-count",
		Summary: "Tracks the total shell-script count against the reduction target.",
		Setup:   setupScriptCount,
	})
}

// setupScriptCount is the program-level monotonic ceiling on canonical .sh count
// (Script Minimization Program T0.1). Excludes .claude/worktrees/ and trash/
// (EC-1). --update-baseline ratchets the ceiling DOWN to current (never up).
func setupScriptCount(fs *flag.FlagSet) Check {
	baselinePath := fs.String("baseline", "infra/quality/script-count-baseline.json", "baseline JSON ceiling")
	update := fs.Bool("update-baseline", false, "ratchet the ceiling down to current count (write)")
	return func(inv Invocation) (verdict.Result, error) {
		data, err := os.ReadFile(*baselinePath) // #nosec G304 -- repo-internal baseline path
		if err != nil {
			return nil, err
		}
		base, err := scriptcount.ParseBaseline(data)
		if err != nil {
			return nil, err
		}
		v, err := scriptcount.Check(inv.Root, base)
		if err != nil {
			return nil, err
		}
		if *update {
			r, err := scriptcount.Ratchet(*baselinePath, v)
			if err != nil {
				return nil, err
			}
			if r.Verdict() == verdict.Fail {
				inv.Stderr.Printf("script-count: refuse to ratchet UP (%d > baseline %d)\n", r.Count, r.Baseline)
				return r, nil
			}
			inv.Stdout.Printf("script-count: baseline ratcheted to %d\n", r.Count)
			return r, nil
		}
		switch {
		case v.Verdict() == verdict.Fail:
			inv.Stderr.Printf("script-count: FAIL — %d canonical .sh > baseline %d (a PR may never increase the count; see 2026-05-24-script-minimization-program-plan)\n", v.Count, v.Baseline)
		case v.Improved:
			inv.Stdout.Printf("script-count: PASS — %d canonical .sh (< baseline %d; run --update-baseline to ratchet)\n", v.Count, v.Baseline)
		default:
			inv.Stdout.Printf("script-count: PASS — %d canonical .sh (== baseline %d)\n", v.Count, v.Baseline)
		}
		return v, nil
	}
}
