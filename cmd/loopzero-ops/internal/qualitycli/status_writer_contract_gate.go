// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/statuswritercontract"
)

func init() {
	register(Gate{
		Name:    "status-writer-contract",
		Summary: "Refuses a bare Status().Update anywhere in operators production code, including the local-alias and SubResource spellings (ADR 2026-08-12-status-writer-contract, B-085).",
		Setup:   setupStatusWriterContract,
	})
}

// setupStatusWriterContract — B-085 bala 4.
//
// The gate the migration asked for in the same breath as the conversion, and
// that for a while did not exist: an adversarial audit injected a fresh bare
// Update into a package with no per-package ratchet and the whole operators
// suite stayed green. A ratchet protects the package somebody remembered; a
// walk fails on the ABSENCE of protection.
//
// Decision: MEET-ASYNC-AMENDMENT-2026-08-14-status-writer-gate
func setupStatusWriterContract(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		o, err := statuswritercontract.Check(statuswritercontract.Config{Root: inv.Root})
		inv.Stdout.Println(o.Summary())
		for _, v := range o.Violations {
			if v.Kind == statuswritercontract.UncoveredSummary {
				inv.Stdout.Printf("  %s:%d — the package of %s() persists status and folds no readiness summary, "+
					"in-line or deferred (ADR-0025 D3)\n", v.File, v.Line, v.Func)
				continue
			}
			inv.Stdout.Printf("  %s:%d — %s() persists status with Update (reached by %s)\n",
				v.File, v.Line, v.Func, v.Form)
		}
		if err != nil {
			return Fail, err
		}
		if !o.Pass {
			inv.Stderr.Println(statuswritercontract.ActionableFix)
			return Fail, nil
		}
		return Pass, nil
	}
}
