// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/silentsurface"
)

func init() {
	register(Gate{
		Name:    "silent-surface",
		Summary: "Ratchet: refuses a NEW router surface guarded by `if cfg.X != nil` that does not report whether it was registered (B-104). Baseline may fall, never rise.",
		Setup:   setupSilentSurface,
	})
}

// setupSilentSurface refuses NEW HTTP surfaces that vanish without saying so.
//
// The router decides fifty times whether to register a surface and, until B-104,
// threw every decision away — measured on the droplet as a 404 on the GitHub
// webhook while /health answered 405, with nothing anywhere reporting it.
//
// Converting all fifty in one commit was refused: router.go is the single
// surface where the route set is read (Unbreakable Rule 5), so an error in any
// one edit would be indistinguishable from the other forty-nine. This gate does
// what unhomed-logic does instead — counts the debt and forbids it from growing.
//
// Decision: MEET-ASYNC-AMENDMENT-2026-08-16-observable-silent-surfaces
func setupSilentSurface(fs *flag.FlagSet) Check {
	// --report lists the debt instead of gating it. Same affordance
	// `unhomed-logic` carries, and for the same reason: a monotonic baseline is
	// only actionable if somebody can see WHICH items compose it. Without it the
	// only way to read the list is to make the gate fail.
	report := fs.Bool("report", false, "list the mute surfaces (no gate)")
	return func(inv Invocation) (Outcome, error) {
		v, err := silentsurface.Check(silentsurface.Config{Root: inv.Root})
		if err != nil {
			return Fail, err
		}
		if *report {
			fmt.Fprintf(inv.Stdout, "%d silent surface(s) in %d route guard(s) (baseline %d):\n",
				len(v.Silent), v.Guarded, v.Baseline)
			for _, s := range v.Silent {
				fmt.Fprintf(inv.Stdout, "  %s:%d  cfg.%s\n", silentsurface.RouterPath, s.Line, s.Field)
			}
			return Pass, nil
		}
		fmt.Fprintln(inv.Stdout, v.Summary())
		if !v.Pass {
			for _, s := range v.Silent {
				fmt.Fprintf(inv.Stdout, "  %s:%d — cfg.%s registers a route and does not report the decision\n",
					silentsurface.RouterPath, s.Line, s.Field)
			}
			fmt.Fprintln(inv.Stderr, silentsurface.ActionableFix)
			return Fail, nil
		}
		return Pass, nil
	}
}
