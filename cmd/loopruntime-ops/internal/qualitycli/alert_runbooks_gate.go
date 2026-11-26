// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/alertrunbooks"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "alert-runbooks",
		Summary: "Every Prometheus alert has a linked runbook annotation.",
		// The gate is right; the tree is not. Fix the finding, then wire.
		// Re-measured 2026-08-06 with the counts, so the size of each debt is on
		// the record rather than implied.
		//
		// 33 alerts without runbook_url. Worth knowing before "just add the
		// annotation": the URLs the conformant alerts already carry are 404 —
		// https://docs.getloopscale.com/runbooks/vault-auth-failure-spike returns 404
		// while the site root returns 200. The gate checks that the string is
		// present, not that a runbook exists, so adding 33 more dead links would
		// buy a green gate and nothing else. An on-call operator following a 404 at
		// 3am is worse off than one who knows there is no runbook.
		// Two corrections measured on 2026-08-19 (B-161, B-164), because the paragraph
		// above told half the story:
		//
		//  1. The gate ALREADY tells a link apart from a runbook, in the part that can be
		//     proven without a network: a path in this repository that does not exist on
		//     disk, and a GitHub URL in an org other than the canonical one (`getloopscale`,
		//     taken from the module path in the go.mod files). What it cannot assert —
		//     whether `docs.getloopscale.com/runbooks/x` is live — is COUNTED as unverifiable
		//     instead of silenced.
		//
		//  2. "33 alerts" was the number for ONE directory. B-164 closed that scope: the
		//     gate sweeps the FOUR directories of `DefaultRulesDirs`. Re-measured 2026-09-06
		//     (B-067): 110 + 86 + 19 + 18 = **233 sites, and the gate sees all 233** — the
		//     "47%" above described the state BEFORE the fix and stayed in the present
		//     tense after it.
		//
		//  3. Sites and alerts are not the same number, and confusing them inflates the
		//     debt. The 233 sites are **145 distinct alerts**: `infra/helm/...` and
		//     `charts/...` are mirrored copies, so 88 sites repeat an alert already counted.
		//     In the failing part, the difference is 46 sites for **37 alerts**. The gate's
		//     message now states both numbers, and the violation carries the directory —
		//     without it, the 9 mirrored pairs came out as identical lines and fixing one
		//     copy left the other failing with the same string.
		Wiring: failingGate,
		Setup:  setupAlertRunbooks,
	})
}

func setupAlertRunbooks(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		o := alertrunbooks.Check(alertrunbooks.Config{Root: inv.Root})
		if o.Verdict() == verdict.Fail {
			for _, v := range o.Violations {
				inv.Stdout.Printf("RUNBOOK-MISSING VIOLATION: %s\n", v)
			}
			// SITES and ALERTS are different numbers, and the old message said "alert(s)"
			// while counting sites. Nine alert files exist twice in the tree
			// (`infra/helm/...` and the mirrored copy in `charts/...`), so the site count
			// is higher than the alert count — measured 2026-09-06: 46 sites, 37 alerts.
			// Calling 46 "alerts without runbook" inflates the debt by 24%.
			inv.Stderr.Printf("\nFAIL — %d alert(s) without runbook annotation, in %d site(s).\n",
				o.DistinctAlerts(), len(o.Violations))
			if o.DistinctAlerts() != len(o.Violations) {
				inv.Stderr.Printf("(%d site(s) are mirrored copies of the same alert — fix every site.)\n",
					len(o.Violations)-o.DistinctAlerts())
			}
			inv.Stderr.Println("To fix: add a `runbook_url:` annotation linking to recovery docs, OR add the")
			inv.Stderr.Println("alert name to ExceptionList (cmd/loopruntime-ops/internal/quality/alertrunbooks) with an explanation.")
			return o, nil
		}
		inv.Stdout.Println("PASS — every alert has a runbook (or is in the exception list).")
		return o, nil
	}
}
