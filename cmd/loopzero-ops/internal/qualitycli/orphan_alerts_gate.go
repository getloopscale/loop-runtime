// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/orphanalerts"
)

func init() {
	register(Gate{
		Name:    "orphan-alerts",
		Summary: "Detects Prometheus alerts with no owning dashboard/runbook.",
		Setup:   setupOrphanAlerts,
	})
}

func setupOrphanAlerts(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		o, err := orphanalerts.Check(orphanalerts.Config{Root: inv.Root})
		if err != nil {
			return Fail, err
		}
		if !o.Pass {
			inv.Stderr.Printf("FAIL — %d orphan alert(s) (vector(0) > 1 placeholder, metric not emitted):\n", len(o.Violations))
			for _, v := range o.Violations {
				inv.Stderr.Printf("  ORPHAN-ALERT VIOLATION: %s\n", v)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("Either wire the real metric + rewrite the expr, OR add the alert name to")
			inv.Stderr.Println("ExceptionList (cmd/loopzero-ops/internal/quality/orphanalerts) with a comment + linked work item.")
			return Fail, nil
		}
		inv.Stdout.Println("PASS — no orphan alerts.")
		return Pass, nil
	}
}
