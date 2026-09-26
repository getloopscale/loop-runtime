// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/liveprobescript"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// liveProbeScriptCheck runs one live probe script contract
// (quality/liveprobescript). Every such gate shares the output shape and the
// verdicts: a violated assertion is a violation; an error with no assertion
// behind it means the contract could not be evaluated.
func liveProbeScriptCheck(spec liveprobescript.Spec) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := liveprobescript.Check(inv.Root, spec)
		if err != nil {
			if len(outcome.Failures) == 0 {
				return Fail, err
			}
			inv.Stderr.Printf("%s: FAIL — %d assertion(s) violated:\n", spec.Gate, len(outcome.Failures))
			for _, v := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", v)
			}
			return Fail, nil
		}
		n := spec.Assertions()
		inv.Stdout.Printf("%s: PASS — %d/%d hermetic assertions on %s\n", spec.Gate, n, n, spec.Script)
		return Pass, nil
	}
}
