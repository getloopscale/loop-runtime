// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/drilloutputs"
)

func init() {
	register(Gate{
		Name:    "drill-outputs-consumers",
		Summary: "Verifies drill Pulumi-output consumers use the canonical schema.",
		Setup:   setupDrillOutputsConsumers,
	})
}

func setupDrillOutputsConsumers(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		o, err := drilloutputs.Check(drilloutputs.Config{Root: inv.Root})
		if err != nil {
			return Fail, err
		}
		if !o.Pass {
			inv.Stderr.Printf("✗ Found %d legacy drill-outputs consumer reference(s):\n", len(o.Findings))
			for _, f := range o.Findings {
				inv.Stderr.Printf("  %s\n", f)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("Each match MUST use the v2 FLAT NAMED EXPORTS schema: replace nested")
			inv.Stderr.Println("cluster/pg/bucket access with direct named outputs, e.g. mgmtClusterKubeconfig,")
			inv.Stderr.Println("pgHost, bucketBuilds (pulumi stack output <name>).")
			inv.Stderr.Println("Per L2-unlock plan T1.3 + EC-02. See finding-2026-05-16-pulumi-outputs-rca.md.")
			return Fail, nil
		}
		inv.Stdout.Println("✓ No legacy drill-outputs consumers found.")
		return Pass, nil
	}
}
