// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"context"
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/dashboardpromtool"
)

func init() {
	register(Gate{
		Name:    "dashboard-promtool",
		Summary: "Validates Grafana dashboard + Prometheus rules via promtool.",
		NoRoot:  true,
		Setup:   setupDashboardPromtool,
	})
}

// setupDashboardPromtool validates Grafana dashboards JSON syntax (always)
// and PromQL queries via promtool (skip-on-missing). T4.3 of plan
// 2026-05-26-maturity-3-62-to-4-0-roadmap.
func setupDashboardPromtool(fs *flag.FlagSet) Check {
	dashboardsDir := fs.String("dir", "infra/helm/monitoring/dashboards", "dashboard directory to scan")
	return func(inv Invocation) (Outcome, error) {
		outcome, err := dashboardpromtool.Check(context.Background(),
			dashboardpromtool.Config{DashboardsDir: *dashboardsDir})
		if err != nil {
			return Fail, err
		}
		if len(outcome.SyntaxErrors) > 0 {
			fmt.Fprintf(inv.Stderr, "dashboard-promtool: FAIL — %d JSON syntax errors:\n", len(outcome.SyntaxErrors))
			for _, e := range outcome.SyntaxErrors {
				fmt.Fprintf(inv.Stderr, "  %s\n", e)
			}
			return Fail, nil
		}
		if len(outcome.PromtoolErrors) > 0 {
			fmt.Fprintf(inv.Stderr, "dashboard-promtool: FAIL — %d PromQL parse errors:\n", len(outcome.PromtoolErrors))
			for _, e := range outcome.PromtoolErrors {
				fmt.Fprintf(inv.Stderr, "  %s\n", e)
			}
			return Fail, nil
		}
		if outcome.PromtoolSkipped {
			fmt.Fprintf(inv.Stdout, "dashboard-promtool: PASS (JSON syntax %d/%d) — promtool SKIP (%s)\n",
				outcome.TotalScanned, outcome.TotalScanned, outcome.PromtoolReason)
		} else {
			fmt.Fprintf(inv.Stdout, "dashboard-promtool: PASS — %d dashboards (JSON syntax + PromQL semantic)\n",
				outcome.TotalScanned)
		}
		return Pass, nil
	}
}
