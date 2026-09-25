// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

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
			fmt.Fprintf(inv.Stderr, "FAIL — %d orphan alert(s) (vector(0) > 1 placeholder, metric not emitted):\n", len(o.Violations))
			for _, v := range o.Violations {
				fmt.Fprintf(inv.Stderr, "  ORPHAN-ALERT VIOLATION: %s\n", v)
			}
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintln(inv.Stderr, "Either wire the real metric + rewrite the expr, OR add the alert name to")
			fmt.Fprintln(inv.Stderr, "ExceptionList (cmd/loopzero-ops/internal/quality/orphanalerts) with a comment + linked work item.")
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "PASS — no orphan alerts.")
		return Pass, nil
	}
}
