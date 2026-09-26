// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/vaultpromalerts"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "vault-prom-alerts",
		Summary: "T8.4 hermetic contract for PrometheusRule vault-audit-alerts: YAML parses + all 4 P1 alerts declared (VaultAuthFailureRateHigh + VaultPolicyDenialRateHigh + VaultSealedOrDown + VaultAuditLogStalled) + all severity=critical + all runbook_url annotations + all team=platform-security + all domain=vault + VaultAuditLogStalled carries invariant \"audit-log-must-flow\" (high-confidence adversary signal). M-Migrate2 2026-07-10 M16 T3.3 Wave 2 replaces infra/tests/check-vault-prom-alerts_test.sh.",
		Setup:   setupVaultPromAlerts,
	})
}

// setupVaultPromAlerts enforces T8.4 PrometheusRule vault-audit-alerts contract.
// Migrated from infra/tests/check-vault-prom-alerts_test.sh
// (M-Migrate2 2026-07-10 M16 T3.3 Wave 2).
func setupVaultPromAlerts(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := vaultpromalerts.Check(vaultpromalerts.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("vault-prom-alerts: FAIL — %d assertion(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", f)
			}
			return outcome, nil
		}
		inv.Stdout.Println("vault-prom-alerts: PASS — 4 P1 alerts declared with severity=critical + runbook_url + team=platform-security + domain=vault (+ invariant marker on VaultAuditLogStalled)")
		return outcome, nil
	}
}
