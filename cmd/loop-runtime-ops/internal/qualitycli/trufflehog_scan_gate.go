// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"context"
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/trufflehogscan"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "trufflehog-scan",
		Summary: "Refuses a new unadjudicated secret candidate, a live secret, or a scan that could not run.",
		Setup:   setupTrufflehogScan,
	})
}

// setupTrufflehogScan wraps `trufflehog git --only-verified --fail` as
// a quality gate. T4.7 of plan. Same skip-on-missing pattern as trivy.
func setupTrufflehogScan(fs *flag.FlagSet) Check {
	baseline := fs.String("baseline", "", "adjudicated baseline path (default: <root>/"+trufflehogscan.BaselineRelPath+")")
	write := fs.Bool("write", false, "regenerate the adjudicated baseline (refuses while any candidate is live)")
	return func(inv Invocation) (verdict.Result, error) {
		if *write {
			gen, err := trufflehogscan.Generate(context.Background(), trufflehogscan.Config{
				RepoRoot: inv.Root, BaselinePath: *baseline,
			})
			if err != nil {
				return nil, err
			}
			inv.Stdout.Printf("trufflehog-scan: baseline rewritten — %d candidate(s) adjudicated\n", gen.Candidates)
			return gen, nil
		}

		out, err := trufflehogscan.Check(context.Background(), trufflehogscan.Config{
			RepoRoot: inv.Root, BaselinePath: *baseline,
		})
		// NO branch returns 0 without a verdict. The three paths that used to end in
		// `exit 0` without knowing whether a secret was present — missing binary, run
		// error, and `--only-verified` with no network — are errors, and exit 5 (B-143).
		if err != nil {
			return nil, err
		}
		if out.Verdict() == verdict.Fail {
			inv.Stderr.Printf("trufflehog-scan: FAIL — %v\n", out.Violation)
			return out, nil
		}
		inv.Stdout.Printf("trufflehog-scan: PASS — %s\n", out.Reason)
		// Not a failure, and not always staleness: the baseline is generated with
		// --no-verification, a strict superset of verified mode (measured 67 ⊇ 65). An
		// entry with no matching finding may be a candidate verification discards — and
		// its being here is exactly why a network outage does not turn into a false red.
		for _, e := range out.StaleEntries {
			inv.Stdout.Printf("  in the baseline with no finding this pass (not a failure; the baseline is a superset): %s [%s] %s\n",
				e.File, e.Detector, e.ID)
		}
		return out, nil
	}
}
