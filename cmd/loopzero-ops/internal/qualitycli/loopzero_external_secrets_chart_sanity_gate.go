// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"errors"
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/esochartsanity"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "loopzero-external-secrets-chart-sanity",
		Summary: "iter 23 chart-sanity contract for infra/helm/loopzero-external-secrets: renders 5 canonical envs (vault-local/standalone-multi/dev/drill/prod) + inventory-based ExternalSecret count parity + metadata.name uniqueness + refreshInterval + loopzero-secrets-secrets ClusterSecretStore refs + vaultBinaryOverride api/secrets/* resolution (≥3) + envName empty fail-fast + no operators/gitea path leak. 8 assertions. Advisory when helm absent. M-Migrate2 2026-07-10 M16 T3.5 Wave 2 replaces infra/tests/check-loopzero-external-secrets-chart-sanity_test.sh.",
		Setup:   setupLoopZeroExternalSecretsChartSanity,
	})
}

// setupLoopZeroExternalSecretsChartSanity enforces the iter 23 chart-sanity contract for
// infra/helm/loopzero-external-secrets. 8 assertions across 5 canonical envs.
// Migrated from infra/tests/check-loopzero-external-secrets-chart-sanity_test.sh
// (M-Migrate2 2026-07-10 M16 T3.5 Wave 2).
func setupLoopZeroExternalSecretsChartSanity(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := esochartsanity.Check(esochartsanity.Config{RepoRoot: inv.Root})
		if errors.Is(err, esochartsanity.ErrChartMissing) {
			inv.Stderr.Println("loopzero-external-secrets-chart-sanity: FAIL — loopzero-external-secrets chart missing")
			return Fail, err
		}
		if err != nil {
			if !isViolation(err, esochartsanity.ErrViolations) {
				return Fail, err
			}
			inv.Stderr.Printf("loopzero-external-secrets-chart-sanity: FAIL — %d assertion(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", f)
			}
			return Fail, nil
		}
		inv.Stdout.Println("loopzero-external-secrets-chart-sanity: PASS — 8 chart-sanity assertions across 5 canonical envs clean")
		return Pass, nil
	}
}
