// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/sqlctxprop"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "sqlc-tx-prop",
		Summary: "Enforces pool-bound vs tx-bound query propagation inside withEnvironmentLock.",
		Setup:   setupSqlcTxProp,
	})
}

// setupSqlcTxProp enforces that sqlc-generated calls inside withEnvironmentLock
// use the WithTx(tx) form. T0.4 of plan 2026-05-29-sqlc-migration v1.2.
// Per MEET-2026-05-29-001 distributed-systems-engineer R-DS-1: pool-bound
// form risks bypassing the lock holder's transaction. Rule 6 + Rule 7
// invariant preservation.
func setupSqlcTxProp(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := sqlctxprop.Check(sqlctxprop.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("sqlc-tx-prop: FAIL — %d violation(s) (scanned %d .go files):\n",
				len(outcome.Violations), outcome.Scanned)
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  %s:%d  %s  — %s\n", v.File, v.Line, v.Function, v.Detail)
			}
			inv.Stderr.Printf("\nFix: %s\n", sqlctxprop.ActionableFix())
			return outcome, nil
		}
		inv.Stdout.Printf("sqlc-tx-prop: PASS — %d store file(s), %d transactional function(s); every query runs on the transaction it received\n",
			outcome.Scanned, outcome.TxFunctions)
		return outcome, nil
	}
}
