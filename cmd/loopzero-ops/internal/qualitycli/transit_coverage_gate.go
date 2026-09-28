// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/transitcoverage"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "transit-coverage",
		Summary: "Enforces the OpenBao transit dispatcher invariant (encrypt/decrypt-for-store).",
		Setup:   setupTransitCoverage,
	})
}

// setupTransitCoverage is the Go home for the vault-prefix-format / transit-
// coverage gate (T3.7 of plan 2026-06-02-openbao-adoption v1.3). Enforces
// the FAANG dual-path invariant: Set/ListForResync MUST use the dispatcher
// (encryptForStore/decryptFromStore), never bypass to raw s.encrypt/decrypt.
func setupTransitCoverage(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := transitcoverage.Check(transitcoverage.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		inv.Stderr.Print(outcome.FormatReport())
		if outcome.Verdict() == verdict.Fail {
			return outcome, nil
		}
		return outcome, nil
	}
}
