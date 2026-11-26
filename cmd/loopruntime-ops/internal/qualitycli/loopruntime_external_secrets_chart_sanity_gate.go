// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/esochartsanity"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "loopruntime-external-secrets-chart-sanity",
		Summary: "iter 23 chart-sanity contract for infra/helm/loopruntime-external-secrets: renders 5 canonical envs (vault-local/standalone-multi/dev/drill/prod) + inventory-based ExternalSecret count parity + metadata.name uniqueness + refreshInterval + loopruntime-secrets-secrets ClusterSecretStore refs + vaultBinaryOverride api/secrets/* resolution (≥3) + envName empty fail-fast + no operators/gitea path leak. 8 assertions. Advisory when helm absent. M-Migrate2 2026-07-10 M16 T3.5 Wave 2 replaces infra/tests/check-loopruntime-external-secrets-chart-sanity_test.sh.",
		Setup:   setupLoopRuntimeExternalSecretsChartSanity,
	})
}

// setupLoopRuntimeExternalSecretsChartSanity enforces the iter 23 chart-sanity contract for
// infra/helm/loopruntime-external-secrets. 8 assertions across 5 canonical envs.
// Migrated from infra/tests/check-loopruntime-external-secrets-chart-sanity_test.sh
// (M-Migrate2 2026-07-10 M16 T3.5 Wave 2).
func setupLoopRuntimeExternalSecretsChartSanity(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := esochartsanity.Check(esochartsanity.Config{RepoRoot: inv.Root})
		// A missing chart (esochartsanity.ErrChartMissing) verified nothing; the
		// runner reports it and exits Incomplete.
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("loopruntime-external-secrets-chart-sanity: FAIL — %d assertion(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", f)
			}
			return outcome, nil
		}
		inv.Stdout.Println("loopruntime-external-secrets-chart-sanity: PASS — 8 chart-sanity assertions across 5 canonical envs clean")
		return outcome, nil
	}
}
