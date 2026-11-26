// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/oidccredadminloopruntime"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "oidc-cred-admin-loopruntime",
		Summary: "Rejects fabricated paulo@loopruntime.test/LoopRuntimeDev2026! credentials; canonical is admin@getloopscale.com/loopruntime-dev-password (M-Migrate2 2026-07-07 replaces infra/tests/check-oidc-cred-admin-loopruntime_test.sh).",
		Setup:   setupOIDCCredAdminLoopruntime,
	})
}

// setupOIDCCredAdminLoopruntime enforces the canonical admin@getloopscale.com / loopruntime-dev-password
// credential — rejects fabricated paulo@loopruntime.test / LoopRuntimeDev2026!.
// Migrated from infra/tests/check-oidc-cred-admin-loopruntime_test.sh (M-Migrate2 2026-07-07).
func setupOIDCCredAdminLoopruntime(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := oidccredadminloopruntime.Check(oidccredadminloopruntime.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("oidc-cred-admin-loopruntime: FAIL — %d fabricated credential reference(s):\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			return outcome, nil
		}
		inv.Stdout.Println("oidc-cred-admin-loopruntime: PASS — no fabricated paulo@loopruntime.test/LoopRuntimeDev2026! (canonical: admin@getloopscale.com)")
		return outcome, nil
	}
}
