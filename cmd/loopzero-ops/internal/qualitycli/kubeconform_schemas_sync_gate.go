// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/kubeconformschemasync"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "kubeconform-schemas-sync",
		Summary: "Every committed loopzero.io kubeconform schema is exactly what the generator produces from the current CRDs (#389).",
		Setup:   setupKubeconformSchemasSync,
	})
}

// setupKubeconformSchemasSync fails when a loopzero.io schema under
// infra/kubeconform-schemas/ is missing, stale or orphaned against
// operators/config/crd/bases/ — the third copy of each CRD, which
// crd-semantic-diff does not compare.
func setupKubeconformSchemasSync(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := kubeconformschemasync.Check(kubeconformschemasync.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("%s: FAIL — %d of %d schema(s) out of step with the CRDs:\n", inv.Name, len(outcome.Drift), outcome.Schemas)
			for _, d := range outcome.Drift {
				inv.Stderr.Printf("  %s\n", d)
			}
			inv.Stderr.Printf("\nFix: %s\n", kubeconformschemasync.Fix)
			return outcome, nil
		}
		inv.Stdout.Printf("%s: PASS — %d loopzero.io schema(s) match their CRDs\n", inv.Name, outcome.Schemas)
		return outcome, nil
	}
}
