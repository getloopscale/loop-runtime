// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/lokivaultretention"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "loki-vault-retention",
		Summary: "T8.5 archive CronJob for Loki vault-audit logs (kind + schedule 04:00 UTC monthly + concurrencyPolicy Forbid + runAsNonRoot + capabilities drop ALL + logcli/mc invocation + AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY secretKeyRef + loki-archive-s3-creds Secret + secrets-system NS target). The T8.2 retention rule is owned by loki-retention-schema. M-Migrate2 2026-07-10 M16 T3.1 Wave 2 replaces infra/tests/check-loki-vault-retention_test.sh.",
		Setup:   setupLokiVaultRetention,
	})
}

// setupLokiVaultRetention enforces T8.2 + T8.5 of plan 2026-06-02-openbao-adoption:
// Loki per-NS retention config + archive CronJob hardening. 14 assertions.
// Migrated from infra/tests/check-loki-vault-retention_test.sh
// (M-Migrate2 2026-07-10 M16 T3.1 Wave 2).
func setupLokiVaultRetention(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := lokivaultretention.Check(lokivaultretention.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("loki-vault-retention: FAIL — %d assertion(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", f)
			}
			return outcome, nil
		}
		inv.Stdout.Println("loki-vault-retention: PASS — T8.5 archive CronJob hardening assertions clean")
		return outcome, nil
	}
}
