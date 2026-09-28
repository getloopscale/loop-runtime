// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/publishhygiene"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
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
	return func(inv Invocation) (verdict.Result, error) {
		out, err := publishhygiene.Check(publishhygiene.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if out.Verdict() == verdict.Fail {
			inv.Stderr.Printf("publish-hygiene: FAIL — %v\n", out.Violation)
			for _, f := range out.Findings {
				inv.Stderr.Printf("  %s:%d — %s\n", f.File, f.Line, f.Text)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("Replace the absolute path with `~`, `<repo>` or `<workspace>`, or declare an")
			inv.Stderr.Println("exemption with a reason AND a sunset in internal/quality/publishhygiene. A dated")
			inv.Stderr.Println("record (docs/wiki, plans/completed, baselines, CHANGELOG) is exempt by design:")
			inv.Stderr.Println("rewriting it would destroy the evidence of what was true then.")
			return out, nil
		}
		inv.Stdout.Printf("publish-hygiene: PASS — %d live file(s) with no workstation path, %d exempt with a dated reason\n",
			out.Scanned, out.Exempt)
		return out, nil
	}
}
