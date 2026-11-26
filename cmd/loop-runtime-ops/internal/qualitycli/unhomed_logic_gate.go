// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"errors"
	"flag"
	"fmt"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/unhomedlogic"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "unhomed-logic",
		Summary: "Forbids un-homed operational logic that belongs in a pkg/quality gate.",
		Setup:   setupUnhomedLogic,
	})
}

// setupUnhomedLogic is the unbiased migration tracker: counts un-homed
// operational logic from the allowlist + ratchets a monotonic baseline.
func setupUnhomedLogic(fs *flag.FlagSet) Check {
	allowlist := fs.String("allowlist", "infra/quality/shell-scripts-allowlist.md", "allowlist markdown")
	baselinePath := fs.String("baseline", "infra/quality/unhomed-baseline.json", "baseline JSON")
	report := fs.Bool("report", false, "print per-domain un-homed counts (no gate)")
	ledger := fs.Bool("ledger", false, "emit per-script disposition + evidence (markdown, for Phase-5 review)")
	update := fs.Bool("update-baseline", false, "ratchet the baseline to current (write)")
	fs.Bool("check", true, "ratchet against baseline (default)")
	return func(inv Invocation) (verdict.Result, error) {
		data, absent, err := unhomedlogic.LoadAllowlist(*allowlist)
		if err != nil {
			return nil, err
		}
		if absent {
			inv.Stderr.Printf("unhomed-logic: no allowlist at %s — nothing is exempt\n", *allowlist)
		}
		// Content verification: read each script body to invalidate suspect KEEPs.
		readBody := unhomedlogic.BodiesUnder(inv.Root)
		current := unhomedlogic.Count(data, readBody)

		if *report {
			for _, l := range current.Render() {
				inv.Stdout.Println(l)
			}
			return unhomedlogic.Outcome{Current: current}, nil
		}

		if *ledger {
			entries := unhomedlogic.Ledger(data, readBody)
			inv.Stdout.Println("# Phase-5 Classification Ledger — un-homed operational logic")
			inv.Stdout.Println("")
			inv.Stdout.Println("Proposed disposition per script (operator ratifies). Evidence is mechanical fact.")
			inv.Stdout.Println("")
			inv.Stdout.Println("| Domain | Script | LOC | Python | ShellsLive | Proposed disposition |")
			inv.Stdout.Println("|---|---|---:|:---:|:---:|---|")
			yn := func(b bool) string {
				if b {
					return "yes"
				}
				return "—"
			}
			for _, e := range entries {
				inv.Stdout.Printf("| %s | %s | %d | %s | %s | %s |\n",
					e.Domain, e.Path, e.LOC, yn(e.HasPython), yn(e.ShellsLive), fmt.Sprintf("%s (%s)", e.Disposition, e.Rationale))
			}
			return unhomedlogic.Outcome{Current: current}, nil
		}

		if *update {
			if err := unhomedlogic.WriteBaseline(*baselinePath, current); err != nil {
				return nil, err
			}
			inv.Stdout.Printf("unhomed-logic: baseline ratcheted to %d un-homed\n", current.Total)
			return unhomedlogic.Outcome{Current: current}, nil
		}

		// --check (default): fail if current exceeds baseline (regression).
		baseline, err := unhomedlogic.LoadBaseline(*baselinePath)
		if err != nil {
			var unreadable *unhomedlogic.BaselineReadError
			if errors.As(err, &unreadable) {
				inv.Stderr.Printf("unhomed-logic: no baseline at %s — run --update-baseline first\n", *baselinePath)
			}
			return nil, err
		}
		o := unhomedlogic.Ratchet(current, baseline)
		for _, l := range current.Render() {
			inv.Stdout.Println(l)
		}
		if len(current.SuspectKeeps) > 0 {
			inv.Stderr.Println("unhomed-logic: FAIL — suspect KEEP (script tagged KEEP-* but embeds a Python heredoc = reimplementable logic, not break-glass):")
			for _, s := range current.SuspectKeeps {
				inv.Stderr.Printf("  - %s\n", s)
			}
			inv.Stderr.Println("A KEEP claim must be genuine operator glue. Move the logic to loop-runtime-ops/Go, or drop the KEEP tag.")
			return o, nil
		}
		if o.Verdict() == verdict.Fail {
			inv.Stderr.Println("unhomed-logic: FAIL — un-homed operational logic increased:")
			for _, r := range o.Regressions {
				inv.Stderr.Printf("  - %s\n", r)
			}
			inv.Stderr.Println("Per plan 2026-05-23-operational-logic-capability-architecture: a PR may never")
			inv.Stderr.Println("increase un-homed logic. Home the capability (loop-runtime-ops/declarative/test) or delete it.")
			return o, nil
		}
		inv.Stdout.Printf("unhomed-logic: PASS — %d un-homed (≤ baseline %d)\n", current.Total, baseline.Total)
		return o, nil
	}
}
