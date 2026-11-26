// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/oidccredadminloopruntime"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "oidc-cred-admin-loop-runtime",
		Summary: "Rejects fabricated paulo@loop-runtime.test/LoopRuntimeDev2026! credentials; canonical is admin@getloopscale.com/loop-runtime-dev-password (M-Migrate2 2026-07-07 replaces infra/tests/check-oidc-cred-admin-loop_runtime_test.sh).",
		Setup:   setupOIDCCredAdminLoopruntime,
	})
}

// setupOIDCCredAdminLoopruntime enforces the canonical admin@getloopscale.com / loop-runtime-dev-password
// credential — rejects fabricated paulo@loop-runtime.test / LoopRuntimeDev2026!.
// Migrated from infra/tests/check-oidc-cred-admin-loop_runtime_test.sh (M-Migrate2 2026-07-07).
func setupOIDCCredAdminLoopruntime(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := oidccredadminloopruntime.Check(oidccredadminloopruntime.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("oidc-cred-admin-loop-runtime: FAIL — %d fabricated credential reference(s):\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			return outcome, nil
		}
		inv.Stdout.Println("oidc-cred-admin-loop-runtime: PASS — no fabricated paulo@loop-runtime.test/LoopRuntimeDev2026! (canonical: admin@getloopscale.com)")
		return outcome, nil
	}
}
