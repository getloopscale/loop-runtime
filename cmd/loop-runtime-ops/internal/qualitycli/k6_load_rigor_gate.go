// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/k6loadrigor"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "k6-load-rigor",
		Summary: "k6 load-harness rigor: dropped_iterations Counter threshold in `base` (EC-2), L5 distributed manifest shape (TestRun kind + pinned image + secretKeyRef + no inline secret, EC-1 YAML validity), k6:distributed:l5 task wired, README per-level runner doc. M-Migrate2 2026-07-07 replaces infra/tests/check-k6-load-rigor_test.sh.",
		Setup:   setupK6LoadRigor,
	})
}

// setupK6LoadRigor enforces the k6 load-harness rigor fixes (dropped_iterations
// threshold in base + L5 distributed manifest shape + README per-level doc).
// Migrated from infra/tests/check-k6-load-rigor_test.sh
// (M-Migrate2 continuation 2026-07-07).
func setupK6LoadRigor(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := k6loadrigor.Check(k6loadrigor.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("k6-load-rigor: FAIL — %d assertion(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", f)
			}
			return outcome, nil
		}
		inv.Stdout.Println("k6-load-rigor: PASS — dropped_iterations threshold in base + L5 manifest shape + per-level docs canonical")
		return outcome, nil
	}
}
