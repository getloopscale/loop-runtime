// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/runbookparity"
)

func init() {
	register(Gate{
		Name:    "runbook-parity",
		Summary: "A token-rotation message must not name the ServiceAccount the runbook forbids.",
		Setup:   setupRunbookParity,
	})
}

func setupRunbookParity(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		out, err := runbookparity.Check(runbookparity.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, runbookparity.ErrContradiction) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "runbook-parity: FAIL — %v\n", err)
			for _, f := range out.Findings {
				fmt.Fprintf(inv.Stderr, "  %s:%d names %q; the runbook prescribes %q\n", f.File, f.Line, f.Named, f.Prescribed)
			}
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "runbook-parity: PASS — %s\n", out.Reason)
		return Pass, nil
	}
}
