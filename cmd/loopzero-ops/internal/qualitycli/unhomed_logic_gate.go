// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/unhomedlogic"
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
	return func(inv Invocation) (Outcome, error) {
		data, err := os.ReadFile(*allowlist) // #nosec G304 -- repo-internal allowlist path
		switch {
		case errors.Is(err, iofs.ErrNotExist):
			// AN ABSENT ALLOWLIST DECLARES NO EXEMPTIONS — the same contract
			// `internal/quality/nonewbash` states, and for the same measured reason:
			// this allowlist lives under `.claude/`, which `3d4075ca3` (2026-09-08)
			// removed from tracking, so it exists in no clone and on no runner.
			// Measured 2026-09-16, this read returned 5 and blocked a push.
			//
			// Nothing exempt is the strictest reading, so losing the file cannot
			// weaken the gate. A file that is PRESENT and unreadable stays fatal.
			fmt.Fprintf(inv.Stderr, "unhomed-logic: no allowlist at %s — nothing is exempt\n", *allowlist)
			data = nil
		case err != nil:
			return Fail, err
		}
		// Content verification: read each script body to invalidate suspect KEEPs.
		readBody := func(p string) (string, bool) {
			b, err := os.ReadFile(filepath.Join(inv.Root, p)) // #nosec G304 -- repo-internal script path
			if err != nil {
				return "", false
			}
			return string(b), true
		}
		current := unhomedlogic.Count(string(data), readBody)

		if *report {
			for _, l := range current.Render() {
				fmt.Fprintln(inv.Stdout, l)
			}
			return Pass, nil
		}

		if *ledger {
			entries := unhomedlogic.Ledger(string(data), readBody)
			fmt.Fprintln(inv.Stdout, "# Phase-5 Classification Ledger — un-homed operational logic")
			fmt.Fprintln(inv.Stdout, "")
			fmt.Fprintln(inv.Stdout, "Proposed disposition per script (operator ratifies). Evidence is mechanical fact.")
			fmt.Fprintln(inv.Stdout, "")
			fmt.Fprintln(inv.Stdout, "| Domain | Script | LOC | Python | ShellsLive | Proposed disposition |")
			fmt.Fprintln(inv.Stdout, "|---|---|---:|:---:|:---:|---|")
			yn := func(b bool) string {
				if b {
					return "yes"
				}
				return "—"
			}
			for _, e := range entries {
				fmt.Fprintf(inv.Stdout, "| %s | %s | %d | %s | %s | %s |\n",
					e.Domain, e.Path, e.LOC, yn(e.HasPython), yn(e.ShellsLive), fmt.Sprintf("%s (%s)", e.Disposition, e.Rationale))
			}
			return Pass, nil
		}

		if *update {
			b, _ := json.MarshalIndent(current, "", "  ")
			if err := os.WriteFile(*baselinePath, append(b, '\n'), 0o600); err != nil {
				return Fail, err
			}
			fmt.Fprintf(inv.Stdout, "unhomed-logic: baseline ratcheted to %d un-homed\n", current.Total)
			return Pass, nil
		}

		// --check (default): fail if current exceeds baseline (regression).
		bdata, err := os.ReadFile(*baselinePath) // #nosec G304 -- repo-internal baseline path
		if err != nil {
			fmt.Fprintf(inv.Stderr, "unhomed-logic: no baseline at %s — run --update-baseline first\n", *baselinePath)
			return Fail, err
		}
		var baseline unhomedlogic.Counts
		if err := json.Unmarshal(bdata, &baseline); err != nil {
			return Fail, err
		}
		ok, regressions := unhomedlogic.RatchetCheck(current, baseline)
		for _, l := range current.Render() {
			fmt.Fprintln(inv.Stdout, l)
		}
		if len(current.SuspectKeeps) > 0 {
			fmt.Fprintln(inv.Stderr, "unhomed-logic: FAIL — suspect KEEP (script tagged KEEP-* but embeds a Python heredoc = reimplementable logic, not break-glass):")
			for _, s := range current.SuspectKeeps {
				fmt.Fprintf(inv.Stderr, "  - %s\n", s)
			}
			fmt.Fprintln(inv.Stderr, "A KEEP claim must be genuine operator glue. Move the logic to loopzero-ops/Go, or drop the KEEP tag.")
			return Fail, nil
		}
		if !ok {
			fmt.Fprintln(inv.Stderr, "unhomed-logic: FAIL — un-homed operational logic increased:")
			for _, r := range regressions {
				fmt.Fprintf(inv.Stderr, "  - %s\n", r)
			}
			fmt.Fprintln(inv.Stderr, "Per plan 2026-05-23-operational-logic-capability-architecture: a PR may never")
			fmt.Fprintln(inv.Stderr, "increase un-homed logic. Home the capability (loopzero-ops/declarative/test) or delete it.")
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "unhomed-logic: PASS — %d un-homed (≤ baseline %d)\n", current.Total, baseline.Total)
		return Pass, nil
	}
}
