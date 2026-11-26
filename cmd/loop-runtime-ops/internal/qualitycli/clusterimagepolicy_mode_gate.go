// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/clusterimagepolicymode"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "clusterimagepolicy-mode",
		Summary: "Validates the sigstore ClusterImagePolicy enforcement mode.",
		Setup:   setupClusterImagePolicyMode,
	})
}

func setupClusterImagePolicyMode(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		o := clusterimagepolicymode.Check(clusterimagepolicymode.Config{Root: inv.Root})
		if o.Verdict() == verdict.Fail {
			inv.Stderr.Printf("FAIL — clusterImagePolicy.mode governance violation: %s\n", o.Reason)
			inv.Stderr.Println("")
			inv.Stderr.Println("The deny-flip rejects unsigned images at admission. Either revert mode to")
			inv.Stderr.Println("'warn', OR record the decision in docs/decisions.md referencing the cutover")
			inv.Stderr.Println("(T8.6 / find_infra_008 / deny-flip / deny-mode / mitigation #4).")
			return o, nil
		}
		inv.Stdout.Printf("PASS — %s\n", o.Reason)
		return o, nil
	}
}
