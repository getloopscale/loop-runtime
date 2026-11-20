// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/oidccredadminloopzero"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "oidc-cred-admin-loopzero",
		Summary: "Rejects fabricated paulo@loopzero.test/LoopZeroDev2026! credentials; canonical is admin@getloopscale.com/loopzero-dev-password (M-Migrate2 2026-07-07 replaces infra/tests/check-oidc-cred-admin-loopzero_test.sh).",
		Setup:   setupOIDCCredAdminLoopzero,
	})
}

// setupOIDCCredAdminLoopzero enforces the canonical admin@getloopscale.com / loopzero-dev-password
// credential — rejects fabricated paulo@loopzero.test / LoopZeroDev2026!.
// Migrated from infra/tests/check-oidc-cred-admin-loopzero_test.sh (M-Migrate2 2026-07-07).
func setupOIDCCredAdminLoopzero(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := oidccredadminloopzero.Check(oidccredadminloopzero.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("oidc-cred-admin-loopzero: FAIL — %d fabricated credential reference(s):\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			return outcome, nil
		}
		inv.Stdout.Println("oidc-cred-admin-loopzero: PASS — no fabricated paulo@loopzero.test/LoopZeroDev2026! (canonical: admin@getloopscale.com)")
		return outcome, nil
	}
}
