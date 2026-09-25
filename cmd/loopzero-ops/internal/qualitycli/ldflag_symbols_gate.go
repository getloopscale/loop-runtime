// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/ldflagsymbols"
)

func init() {
	register(Gate{
		Name:    "ldflag-symbols",
		Summary: "Refuses a `-X` ldflag injection whose import path or symbol does not resolve on disk — the linker accepts it silently and the binary keeps its default (B-053).",
		Setup:   setupLdflagSymbols,
	})
}

// setupLdflagSymbols refuses a `-X` injection that points at a symbol which does
// not exist.
//
// `-X` over a missing path or symbol is a no-op that exits 0 — the linker does
// not warn — so the build succeeds and the binary keeps its default. Measured
// (B-053): the api Dockerfile injected into the wrong org AND a package the
// decomposition had moved, and the engine answered `{"version":"dev"}` live.
//
// It RESOLVES rather than matching spelling: the import path against the go.mod
// files on disk, the symbol against the package AST. A grep for the wrong org
// would catch one typo; resolving catches the package move that a legitimate
// refactor introduces.
//
// Decision: MEET-ASYNC-AMENDMENT-2026-08-16-ldflag-symbol-resolution
func setupLdflagSymbols(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		out, err := ldflagsymbols.Check(ldflagsymbols.Config{Root: inv.Root})
		if err != nil {
			return Fail, err
		}
		inv.Stdout.Println(out.Summary())
		if !out.Pass {
			for _, v := range out.Violations {
				inv.Stdout.Printf("  %s:%d — `-X %s.%s`: %s\n", v.File, v.Line, v.ImportPath, v.Symbol, v.Reason)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println(ldflagsymbols.ActionableFix)
			return Fail, nil
		}
		return Pass, nil
	}
}
