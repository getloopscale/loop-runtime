// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/helmoverlapparity"
)

func init() {
	register(Gate{
		Name:    "helm-overlap-parity",
		Summary: "Keeps the dual Helm chart systems (infra/helm vs charts) in parity.",
		Setup:   setupHelmOverlapParity,
	})
}

// setupHelmOverlapParity enforces that the 3 overlapping bootstrap charts
// (infra/helm/{loopzero-api,loopzero-cloud,admission-policies}) never have versions
// AHEAD of their published counterparts (charts/{loopzero-api,loopzero-cloud,
// loopzero-admission-policies}). T1.2 of plan
// 2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md. Closes M06 HIGH gap.
func setupHelmOverlapParity(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := helmoverlapparity.Check(helmoverlapparity.Config{RepoRoot: inv.Root})
		if err != nil {
			return Fail, err
		}
		if !outcome.Pass {
			fmt.Fprintf(inv.Stderr, "helm-overlap-parity: FAIL — %d drift/missing finding(s):\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				fmt.Fprintf(inv.Stderr, "  %s\n", v)
			}
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintln(inv.Stderr, "Fix: align infra/helm/<chart>/Chart.yaml version with charts/<chart>/Chart.yaml,")
			fmt.Fprintln(inv.Stderr, "     OR bump published chart in charts/<chart>/ first.")
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "helm-overlap-parity: PASS — bootstrap charts in parity with (or behind) published")
		return Pass, nil
	}
}
