// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/ma7statuslocality"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "ma7-status-locality",
		Summary: "MA-7 Gates B+C: sqlc status funcs + SafeTransition only in db/store/.",
		Setup:   setupMa7StatusLocality,
	})
}

// setupMa7StatusLocality enforces MA-7 Gates B+C of the M9 db-split: the sqlc
// status UPDATE funcs + <machine>.SafeTransition are referenced only from
// api/internal/db/store/. ADR 2026-07-03-m9-db-store-subpackage-topology D3.
func setupMa7StatusLocality(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := ma7statuslocality.Check(ma7statuslocality.Config{RepoRoot: inv.Root})
		if err != nil {
			return Fail, err
		}
		if !outcome.Pass {
			inv.Stderr.Printf("ma7-status-locality: FAIL \u2014 %d violation(s) (scanned %d .go files):\n", len(outcome.Violations), outcome.Scanned)
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  %s:%d Gate %s \u2014 %s outside db/store/\n", v.File, v.Line, v.Gate, v.Name)
			}
			inv.Stderr.Printf("\nFix: %s\n", ma7statuslocality.ActionableFix())
			return Fail, nil
		}
		inv.Stdout.Printf("ma7-status-locality: PASS \u2014 %d .go files scanned; status machinery confined to db/store/ (MA-7 Gates B+C)\n", outcome.Scanned)
		return Pass, nil
	}
}
