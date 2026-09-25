// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

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
			fmt.Fprintf(inv.Stderr, "✗ Found %d legacy drill-outputs consumer reference(s):\n", len(o.Findings))
			for _, f := range o.Findings {
				fmt.Fprintf(inv.Stderr, "  %s\n", f)
			}
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintln(inv.Stderr, "Each match MUST use the v2 FLAT NAMED EXPORTS schema: replace nested")
			fmt.Fprintln(inv.Stderr, "cluster/pg/bucket access with direct named outputs, e.g. mgmtClusterKubeconfig,")
			fmt.Fprintln(inv.Stderr, "pgHost, bucketBuilds (pulumi stack output <name>).")
			fmt.Fprintln(inv.Stderr, "Per L2-unlock plan T1.3 + EC-02. See finding-2026-05-16-pulumi-outputs-rca.md.")
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "✓ No legacy drill-outputs consumers found.")
		return Pass, nil
	}
}
