// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/agplisolation"
)

func init() {
	register(Gate{
		Name:    "agpl-isolation",
		Summary: "Verifies AGPL-licensed code stays isolated from proprietary import paths.",
		Setup:   setupAGPLIsolation,
	})
}

func setupAGPLIsolation(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		o := agplisolation.Check(agplisolation.Config{Root: inv.Root})
		if !o.Pass {
			fmt.Fprintf(inv.Stderr, "FAIL — %d AGPL violation(s) outside the ADR-0048 allow-list:\n", len(o.Violations))
			for _, v := range o.Violations {
				fmt.Fprintf(inv.Stderr, "  AGPL VIOLATION: %s\n", v)
			}
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintln(inv.Stderr, "AGPL-3.0 has copyleft network-distribution requirements. Either re-implement")
			fmt.Fprintln(inv.Stderr, "with an Apache-2.0 alternative, OR add the import to DefaultAllowlist in")
			fmt.Fprintln(inv.Stderr, "cmd/loopzero-ops/internal/quality/agplisolation with a NEW ADR + legal sign-off.")
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "PASS — no AGPL imports outside the ADR-0048 allow-list.")
		return Pass, nil
	}
}
