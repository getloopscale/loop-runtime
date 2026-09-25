// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/oidccredadminloopzero"
)

func init() {
	register(Gate{
		Name:    "oidc-cred-admin-loopzero",
		Summary: "Rejects fabricated paulo@loopzero.test/LoopZeroDev2026! credentials; canonical is admin@loopzero.example/loopzero-dev-password (M-Migrate2 2026-07-07 replaces infra/tests/check-oidc-cred-admin-loopzero_test.sh).",
		Setup:   setupOIDCCredAdminLoopzero,
	})
}

// setupOIDCCredAdminLoopzero enforces the canonical admin@loopzero.example / loopzero-dev-password
// credential — rejects fabricated paulo@loopzero.test / LoopZeroDev2026!.
// Migrated from infra/tests/check-oidc-cred-admin-loopzero_test.sh (M-Migrate2 2026-07-07).
func setupOIDCCredAdminLoopzero(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := oidccredadminloopzero.Check(oidccredadminloopzero.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, oidccredadminloopzero.ErrViolations) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "oidc-cred-admin-loopzero: FAIL — %d fabricated credential reference(s):\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				fmt.Fprintf(inv.Stderr, "  %s\n", v)
			}
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "oidc-cred-admin-loopzero: PASS — no fabricated paulo@loopzero.test/LoopZeroDev2026! (canonical: admin@loopzero.example)")
		return Pass, nil
	}
}
