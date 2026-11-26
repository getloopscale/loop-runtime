// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/dashboardrootparity"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "dashboard-root-parity",
		Summary: "The dashboard runbook must name the path the edge actually serves.",
		Setup:   setupDashboardRootParity,
	})
}

func setupDashboardRootParity(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		out, err := dashboardrootparity.Check(dashboardrootparity.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if out.Verdict() == verdict.Fail {
			inv.Stderr.Printf("dashboard-root-parity: FAIL — %v\n", out.Violation)
			return out, nil
		}
		inv.Stdout.Printf("dashboard-root-parity: PASS — the edge serves %s and the runbook names it\n", out.Served)
		return out, nil
	}
}
