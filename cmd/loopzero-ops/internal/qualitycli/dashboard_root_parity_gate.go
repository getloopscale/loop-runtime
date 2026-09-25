// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/dashboardrootparity"
)

func init() {
	register(Gate{
		Name:    "dashboard-root-parity",
		Summary: "The dashboard runbook must name the path the edge actually serves.",
		Setup:   setupDashboardRootParity,
	})
}

func setupDashboardRootParity(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		out, err := dashboardrootparity.Check(dashboardrootparity.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, dashboardrootparity.ErrDivergent, dashboardrootparity.ErrUndocumented) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "dashboard-root-parity: FAIL — %v\n", err)
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "dashboard-root-parity: PASS — the edge serves %s and the runbook names it\n", out.Served)
		return Pass, nil
	}
}
