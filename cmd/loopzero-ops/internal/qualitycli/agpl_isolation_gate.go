// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/agplisolation"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "agpl-isolation",
		Summary: "Verifies AGPL-licensed code stays isolated from proprietary import paths.",
		Setup:   setupAGPLIsolation,
	})
}

func setupAGPLIsolation(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		o := agplisolation.Check(agplisolation.Config{Root: inv.Root})
		if o.Verdict() == verdict.Fail {
			inv.Stderr.Printf("FAIL — %d AGPL violation(s) outside the ADR-0048 allow-list:\n", len(o.Violations))
			for _, v := range o.Violations {
				inv.Stderr.Printf("  AGPL VIOLATION: %s\n", v)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("AGPL-3.0 has copyleft network-distribution requirements. Either re-implement")
			inv.Stderr.Println("with an Apache-2.0 alternative, OR add the import to DefaultAllowlist in")
			inv.Stderr.Println("cmd/loopzero-ops/internal/quality/agplisolation with a NEW ADR + legal sign-off.")
			return o, nil
		}
		inv.Stdout.Println("PASS — no AGPL imports outside the ADR-0048 allow-list.")
		return o, nil
	}
}
