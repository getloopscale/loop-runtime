// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/orphanalerts"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "orphan-alerts",
		Summary: "Detects Prometheus alerts with no owning dashboard/runbook.",
		Setup:   setupOrphanAlerts,
	})
}

func setupOrphanAlerts(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		o, err := orphanalerts.Check(orphanalerts.Config{Root: inv.Root})
		if err != nil {
			return nil, err
		}
		if o.Verdict() == verdict.Fail {
			inv.Stderr.Printf("FAIL — %d orphan alert(s) (vector(0) > 1 placeholder, metric not emitted):\n", len(o.Violations))
			for _, v := range o.Violations {
				inv.Stderr.Printf("  ORPHAN-ALERT VIOLATION: %s\n", v)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("Either wire the real metric + rewrite the expr, OR add the alert name to")
			inv.Stderr.Println("ExceptionList (cmd/loopruntime-ops/internal/quality/orphanalerts) with a comment + linked work item.")
			return o, nil
		}
		inv.Stdout.Println("PASS — no orphan alerts.")
		return o, nil
	}
}
