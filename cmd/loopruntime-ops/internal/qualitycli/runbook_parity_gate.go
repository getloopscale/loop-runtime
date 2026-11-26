// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/runbookparity"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "runbook-parity",
		Summary: "A token-rotation message must not name the ServiceAccount the runbook forbids.",
		Setup:   setupRunbookParity,
	})
}

func setupRunbookParity(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		out, err := runbookparity.Check(runbookparity.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if out.Verdict() == verdict.Fail {
			inv.Stderr.Printf("runbook-parity: FAIL — %v\n", out.Violation)
			for _, f := range out.Findings {
				inv.Stderr.Printf("  %s:%d names %q; the runbook prescribes %q\n", f.File, f.Line, f.Named, f.Prescribed)
			}
			return out, nil
		}
		inv.Stdout.Printf("runbook-parity: PASS — %s\n", out.Reason)
		return out, nil
	}
}
