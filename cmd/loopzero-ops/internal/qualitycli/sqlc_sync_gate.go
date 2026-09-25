// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

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
			fmt.Fprintf(inv.Stderr, "sqlc-sync: FAIL — %d violation(s) between committed generated/ and fresh sqlc generate (version %s):\n",
				len(outcome.Violations), outcome.SqlcVersion)
			for _, v := range outcome.Violations {
				fmt.Fprintf(inv.Stderr, "  [%s] %s\n", v.Reason, v.File)
				if v.Detail != "" {
					fmt.Fprintf(inv.Stderr, "%s\n", v.Detail)
				}
			}
			fmt.Fprintf(inv.Stderr, "\nFix: %s\n", sqlcsync.ActionableFix())
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "sqlc-sync: PASS — committed generated/ in-sync with sqlc %s\n", outcome.SqlcVersion)
		return Pass, nil
	}
}
