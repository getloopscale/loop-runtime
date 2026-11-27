// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"context"
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/dashboardpromtool"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
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
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := dashboardpromtool.Check(context.Background(),
			dashboardpromtool.Config{DashboardsDir: *dashboardsDir})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail && len(outcome.SyntaxErrors) > 0 {
			inv.Stderr.Printf("dashboard-promtool: FAIL — %d JSON syntax errors:\n", len(outcome.SyntaxErrors))
			for _, e := range outcome.SyntaxErrors {
				inv.Stderr.Printf("  %s\n", e)
			}
			return outcome, nil
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("dashboard-promtool: FAIL — %d PromQL parse errors:\n", len(outcome.PromtoolErrors))
			for _, e := range outcome.PromtoolErrors {
				inv.Stderr.Printf("  %s\n", e)
			}
			return outcome, nil
		}
		if outcome.PromtoolSkipped {
			inv.Stdout.Printf("dashboard-promtool: PASS (JSON syntax %d/%d) — promtool SKIP (%s)\n",
				outcome.TotalScanned, outcome.TotalScanned, outcome.PromtoolReason)
		} else {
			inv.Stdout.Printf("dashboard-promtool: PASS — %d dashboards (JSON syntax + PromQL semantic)\n",
				outcome.TotalScanned)
		}
		return outcome, nil
	}
}
