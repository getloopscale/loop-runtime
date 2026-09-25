// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"errors"
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/esocreationpolicy"
)

func init() {
	register(Gate{
		Name:    "loopzero-external-secrets-creation-policy",
		Summary: "iter 20 P0 regression: infra/helm/loopzero-external-secrets renders ExternalSecret CRs with creationPolicy: Orphan (NEVER Owner — Owner+blockOwnerDeletion cascade-deletes K8s Secrets on chart uninstall). 4 assertions: ≥1 ES CR + zero Owner + deletionPolicy Retain ≥ Orphan + Orphan==ES CR count. Advisory when helm absent. M-Migrate2 2026-07-10 M16 T3.6 Wave 2 replaces infra/tests/check-loopzero-external-secrets-creation-policy_test.sh.",
		Setup:   setupLoopZeroExternalSecretsCreationPolicy,
	})
}

// setupLoopZeroExternalSecretsCreationPolicy is the iter 20 P0 regression for creationPolicy: Orphan.
// Migrated from infra/tests/check-loopzero-external-secrets-creation-policy_test.sh
// (M-Migrate2 2026-07-10 M16 T3.6 Wave 2).
func setupLoopZeroExternalSecretsCreationPolicy(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := esocreationpolicy.Check(esocreationpolicy.Config{RepoRoot: inv.Root})
		if errors.Is(err, esocreationpolicy.ErrChartMissing) {
			inv.Stderr.Println("loopzero-external-secrets-creation-policy: FAIL — chart missing")
			return Fail, err
		}
		if err != nil {
			if !isViolation(err, esocreationpolicy.ErrViolations) {
				return Fail, err
			}
			inv.Stderr.Printf("loopzero-external-secrets-creation-policy: FAIL — %d assertion(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", f)
			}
			return Fail, nil
		}
		inv.Stdout.Println("loopzero-external-secrets-creation-policy: PASS — 4 assertions (creationPolicy Orphan + Owner=0 + deletionPolicy Retain + count parity) clean")
		return Pass, nil
	}
}
