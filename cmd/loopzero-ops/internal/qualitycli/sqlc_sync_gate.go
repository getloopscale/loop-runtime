// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/sqlcsync"
)

func init() {
	register(Gate{
		Name:    "sqlc-sync",
		Summary: "Committed sqlc generated/ is in sync with the schema codegen.",
		Setup:   setupSqlcSync,
	})
}

// setupSqlcSync verifies committed api/internal/db/generated/ is in-sync with a
// fresh `sqlc generate`. T0.3 of plan 2026-05-29-sqlc-migration v1.2.
// Successor gate to schema_mirror_test.go (deleted in T4.2 per D7 two-step
// after this gate goes green for one CI cycle).
func setupSqlcSync(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := sqlcsync.Check(sqlcsync.Config{RepoRoot: inv.Root})
		if err != nil {
			return Fail, err
		}
		if !outcome.Pass {
			inv.Stderr.Printf("sqlc-sync: FAIL — %d violation(s) between committed generated/ and fresh sqlc generate (version %s):\n",
				len(outcome.Violations), outcome.SqlcVersion)
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  [%s] %s\n", v.Reason, v.File)
				if v.Detail != "" {
					inv.Stderr.Printf("%s\n", v.Detail)
				}
			}
			inv.Stderr.Printf("\nFix: %s\n", sqlcsync.ActionableFix())
			return Fail, nil
		}
		inv.Stdout.Printf("sqlc-sync: PASS — committed generated/ in-sync with sqlc %s\n", outcome.SqlcVersion)
		return Pass, nil
	}
}
