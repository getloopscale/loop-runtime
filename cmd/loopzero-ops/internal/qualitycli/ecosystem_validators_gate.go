// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/ecosystemvalidators"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "ecosystem-validators",
		Summary: "Runs `check_xrefs.py` and `test_e2e_smoke.py`, which the golden-rule change protocol requires green and which no gate ran (B-129). Reports FAIL and WARN SEPARATELY: adding them would let the warning erase the failure.",
		Setup:   setupEcosystemValidators,
	})
}

func setupEcosystemValidators(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		out, err := ecosystemvalidators.Check(ecosystemvalidators.Config{RepoRoot: inv.Root})
		if out.Advisory {
			inv.Stdout.Printf("ecosystem-validators: ADVISORY — nothing was concluded: %s\n", out.AdvisoryReason)
			return Pass, nil
		}
		if err != nil {
			if !isViolation(err, ecosystemvalidators.ErrValidatorFailed) {
				return Fail, err
			}
			inv.Stderr.Printf("ecosystem-validators: FAIL — %v\n", err)
			for _, r := range out.Failed() {
				inv.Stderr.Printf("  %s: %s\n", r.Name, r.Tail)
			}
			return Fail, nil
		}
		// FAIL and WARN are reported separately: adding them up would let the warning
		// erase the failure, and a validator that always warns teaches people to ignore it.
		inv.Stdout.Printf("ecosystem-validators: PASS — %d validator(s) green, %d warning(s)\n",
			len(out.Results), out.Warnings())
		return Pass, nil
	}
}
