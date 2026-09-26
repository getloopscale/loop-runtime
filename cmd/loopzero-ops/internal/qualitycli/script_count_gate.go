// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"
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
			return Fail, err
		}
		base, err := scriptcount.ParseBaseline(data)
		if err != nil {
			return Fail, err
		}
		v, err := scriptcount.Check(inv.Root, base)
		if err != nil {
			return Fail, err
		}
		if *update {
			if v.Count > base.CanonicalSH {
				inv.Stderr.Printf("script-count: refuse to ratchet UP (%d > baseline %d)\n", v.Count, base.CanonicalSH)
				return Fail, nil
			}
			out := fmt.Sprintf("{\n  \"canonical_sh\": %d\n}\n", v.Count)
			if err := os.WriteFile(*baselinePath, []byte(out), 0o644); err != nil { // #nosec G306 -- repo-internal baseline
				return Fail, err
			}
			inv.Stdout.Printf("script-count: baseline ratcheted to %d\n", v.Count)
			return Pass, nil
		}
		if !v.OK {
			inv.Stderr.Printf("script-count: FAIL — %d canonical .sh > baseline %d (a PR may never increase the count; see 2026-05-24-script-minimization-program-plan)\n", v.Count, v.Baseline)
			return Fail, nil
		}
		if v.Improved {
			inv.Stdout.Printf("script-count: PASS — %d canonical .sh (< baseline %d; run --update-baseline to ratchet)\n", v.Count, v.Baseline)
			return Pass, nil
		}
		inv.Stdout.Printf("script-count: PASS — %d canonical .sh (== baseline %d)\n", v.Count, v.Baseline)
		return Pass, nil
	}
}
