// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/tenantpredicate"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "tenant-predicate",
		Summary: "Enforces the multi-tenant isolation predicate (security veto-backed).",
		Setup:   setupTenantPredicate,
	})
}

// setupTenantPredicate enforces that every multi-tenant query carries the
// `tenant_id = $N` predicate (or marks itself exempt). T0.5 of plan
// 2026-05-29-sqlc-migration v1.2. Per platform-security-engineer condition
// 1 (VETO-backed) + M-1 + M-2.
func setupTenantPredicate(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := tenantpredicate.Check(tenantpredicate.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("tenant-predicate: FAIL — %d violation(s) (scanned %d .sql files):\n",
				len(outcome.Violations), outcome.Scanned)
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  [%s] %s @ %s:%s — %s\n", v.Reason, v.Query, v.File, v.Table, v.Detail)
			}
			inv.Stderr.Printf("\nFix: %s\n", tenantpredicate.ActionableFix())
			return outcome, nil
		}
		inv.Stdout.Printf("tenant-predicate: PASS — %d .sql files scanned; every multi-tenant query carries tenant_id\n", outcome.Scanned)
		return outcome, nil
	}
}
