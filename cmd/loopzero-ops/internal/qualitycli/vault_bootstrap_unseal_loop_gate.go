// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/vaultbootstrapunsealloop"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "vault-bootstrap-unseal-loop",
		Summary: "Regression guard for the 2026-06-15 canonical fix of vault-bootstrap-dev.sh Steps 2+3 (rc-independent bao-status parse, capitalized 'True' compare, no lowercase 'true' fallback). M-Migrate2 2026-07-07 replaces infra/tests/check-vault-bootstrap-unseal-loop_test.sh.",
		Setup:   setupVaultBootstrapUnsealLoop,
	})
}

// setupVaultBootstrapUnsealLoop enforces the 2026-06-15 canonical fix of
// infra/scripts/local/vault-bootstrap-dev.sh Steps 2 + 3 (rc-independent
// bao-status parse, capitalized "True" compare, no lowercase fallback trap).
// Migrated from infra/tests/check-vault-bootstrap-unseal-loop_test.sh
// (M-Migrate2 continuation 2026-07-07).
func setupVaultBootstrapUnsealLoop(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := vaultbootstrapunsealloop.Check(vaultbootstrapunsealloop.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("vault-bootstrap-unseal-loop: FAIL — %d assertion(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", f)
			}
			return outcome, nil
		}
		inv.Stdout.Println("vault-bootstrap-unseal-loop: PASS — canonical fix preserved (rc-independent parse, capitalized True compare, anti-patterns absent)")
		return outcome, nil
	}
}
