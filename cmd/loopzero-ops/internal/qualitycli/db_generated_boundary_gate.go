// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/dbgeneratedboundary"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "db-generated-boundary",
		Summary: "MA-7 Gate A: no services/routes/server imports internal/db/generated.",
		Setup:   setupDBGeneratedBoundary,
	})
}

// setupDBGeneratedBoundary enforces MA-7 Gate A of the M9 db-split: no file under
// api/internal/{services,routes,server} imports api/internal/db/generated.
// ADR 2026-07-03 m9-db-store-subpackage-topology, decision D3.
func setupDBGeneratedBoundary(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := dbgeneratedboundary.Check(dbgeneratedboundary.Config{RepoRoot: inv.Root})
		if err != nil {
			return Fail, err
		}
		if !outcome.Pass {
			inv.Stderr.Printf("db-generated-boundary: FAIL \u2014 %d violation(s) (scanned %d .go files):\n",
				len(outcome.Violations), outcome.Scanned)
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  %s:%d imports %s\n", v.File, v.Line, v.Import)
			}
			inv.Stderr.Printf("\nFix: %s\n", dbgeneratedboundary.ActionableFix())
			return Fail, nil
		}
		for _, d := range outcome.MissingDirs {
			inv.Stderr.Printf("db-generated-boundary: WARN \u2014 fenced dir %q absent from tree (a rename may have silently dropped a fence)\n", d)
		}
		inv.Stdout.Printf("db-generated-boundary: PASS \u2014 %d .go files scanned; no outer-layer imports of db/generated (MA-7 Gate A), and none of the hand-written db package in the layers where that fence applies\n", outcome.Scanned)
		return Pass, nil
	}
}
