// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/vaultbootstrapunsealloop"
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
	return func(inv Invocation) (Outcome, error) {
		outcome, err := vaultbootstrapunsealloop.Check(vaultbootstrapunsealloop.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, vaultbootstrapunsealloop.ErrViolations) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "vault-bootstrap-unseal-loop: FAIL — %d assertion(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				fmt.Fprintf(inv.Stderr, "  %s\n", f)
			}
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "vault-bootstrap-unseal-loop: PASS — canonical fix preserved (rc-independent parse, capitalized True compare, anti-patterns absent)")
		return Pass, nil
	}
}
