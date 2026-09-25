// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/publishhygiene"
)

func init() {
	register(Gate{
		Name:    "publish-hygiene",
		Summary: "No LIVE versioned file carries a workstation path. A dated record is exempt by design (docs/README.md forbids rewriting it). The IP half of the sibling loopzero-rag gate was NOT carried over: against this tree it produced 104 findings, almost all false (version strings, Cloudflare CIDRs, the declared live target). B-130.",
		Setup:   setupPublishHygiene,
	})
}

// setupPublishHygiene refuses a LIVE versioned file that carries a workstation path.
//
// Of the 20 sibling repositories measured on 2026-08-17, ONE had this guard (loopzero-rag) —
// and that is why the kit's leak surfaced there. In the other 19 it aged in silence.
// This repository publishes OCI charts and images, and published history is public
// RETROACTIVELY.
func setupPublishHygiene(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		out, err := publishhygiene.Check(publishhygiene.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, publishhygiene.ErrLeak) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "publish-hygiene: FAIL — %v\n", err)
			for _, f := range out.Findings {
				fmt.Fprintf(inv.Stderr, "  %s:%d — %s\n", f.File, f.Line, f.Text)
			}
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintln(inv.Stderr, "Replace the absolute path with `~`, `<repo>` or `<workspace>`, or declare an")
			fmt.Fprintln(inv.Stderr, "exemption with a reason AND a sunset in internal/quality/publishhygiene. A dated")
			fmt.Fprintln(inv.Stderr, "record (docs/wiki, plans/completed, baselines, CHANGELOG) is exempt by design:")
			fmt.Fprintln(inv.Stderr, "rewriting it would destroy the evidence of what was true then.")
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "publish-hygiene: PASS — %d live file(s) with no workstation path, %d exempt with a dated reason\n",
			out.Scanned, out.Exempt)
		return Pass, nil
	}
}
